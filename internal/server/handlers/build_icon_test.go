package handlers

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"howett.net/plist"

	"github.com/openprx/fenfa/internal/config"
	"github.com/openprx/fenfa/internal/store"
)

func iconPNG(t *testing.T, shade color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA(x, y, shade)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func iconArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestUploadExtractsBuildIconAndPreservesArchive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{"ios", "android"} {
		t.Run(platform, func(t *testing.T) {
			withUploadWorkdir(t)
			db := openHandlerTestDB(t)
			variant := seedVariant(t, db, platform, "com.example.app")
			red := color.NRGBA{R: 255, A: 255}
			blue := color.NRGBA{B: 255, A: 255}
			green := color.NRGBA{G: 255, A: 255}
			var previousIcon []byte
			for step, shade := range []color.NRGBA{red, blue, green, red, red} {
				icon := iconPNG(t, shade)
				entries := map[string][]byte{}
				filename := "demo.apk"
				if platform == "ios" {
					filename = "demo.ipa"
					// A nonstandard name verifies that extraction follows the primary plist.
					info := map[string]interface{}{"CFBundleIcons": map[string]interface{}{
						"CFBundlePrimaryIcon": map[string]interface{}{"CFBundleIconFiles": []string{"Brand.png"}},
					}}
					data, err := plist.Marshal(info, plist.BinaryFormat)
					if err != nil {
						t.Fatal(err)
					}
					entries["Payload/Demo.app/Info.plist"] = data
					entries["Payload/Demo.app/Brand@3x.png"] = icon
					entries["Payload/Demo.app/PlugIns/Widget.appex/Icon.png"] = iconPNG(t, blue)
				} else {
					entries["AndroidManifest.xml"] = []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android"><application android:icon="@mipmap/brand"/></manifest>`)
					entries["res/mipmap-xxxhdpi/brand.png"] = icon
					entries["res/mipmap-mdpi/ic_launcher.png"] = iconPNG(t, blue)
				}
				fields := map[string]string{"variant_id": variant.ID, "version": "1.0", "build": "1"}
				switch step {
				case 1:
					fields["icon_base64"] = "invalid base64"
				case 2:
					fields["icon_base64"] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(icon)
					// An explicitly supplied icon takes precedence over the build's icon.
					for name := range entries {
						if strings.HasSuffix(name, ".png") {
							entries[name] = iconPNG(t, blue)
						}
					}
				case 3:
					// No icon: a new release must retain the last good product icon.
					entries = map[string][]byte{"readme.txt": []byte("no icon")}
				case 4:
					// An unreadable archive is still accepted by the existing upload contract.
					entries = nil
				}
				archive := iconArchive(t, entries)
				if step == 4 {
					archive = []byte("PK\x03\x04broken archive")
				}
				rr := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rr)
				c.Request = makeMultipartRequest(t, "/upload", fields, filename, archive)
				Upload(db, config.Default())(c)
				if rr.Code != http.StatusCreated {
					t.Fatalf("step %d: status %d: %s", step, rr.Code, rr.Body.String())
				}
				var product store.Product
				if err := db.First(&product, "id = ?", variant.ProductID).Error; err != nil {
					t.Fatal(err)
				}
				stored, err := os.ReadFile(product.IconPath)
				if err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(stored))
				if err != nil {
					t.Fatal(err)
				}
				if step < 3 {
					if got := color.NRGBAModel.Convert(img.At(0, 0)); got != shade {
						t.Fatalf("step %d: wrong icon: %v", step, got)
					}
				} else if !bytes.Equal(previousIcon, stored) {
					t.Fatalf("step %d: replaced existing icon", step)
				}
				previousIcon = stored
				iconRR := httptest.NewRecorder()
				iconContext, _ := gin.CreateTestContext(iconRR)
				iconContext.Params = gin.Params{{Key: "productID", Value: product.ID}}
				ProductIcon(db)(iconContext)
				if iconRR.Code != http.StatusOK || !bytes.Equal(iconRR.Body.Bytes(), stored) {
					t.Fatal("product icon is not served correctly")
				}
				var releases []store.Release
				if err := db.Find(&releases, "variant_id = ?", variant.ID).Error; err != nil {
					t.Fatal(err)
				}
				digest := sha256.Sum256(archive)
				found := false
				for _, release := range releases {
					if release.SHA256 != hex.EncodeToString(digest[:]) {
						continue
					}
					found = true
					original, err := os.ReadFile(release.StoragePath)
					if err != nil || !bytes.Equal(original, archive) || release.SizeBytes != int64(len(archive)) {
						t.Fatal("original build was changed during icon extraction")
					}
				}
				if !found {
					t.Fatal("uploaded archive SHA256 was not preserved")
				}
			}
		})
	}
}

func TestDecodeUploadIconRejectsInvalidImages(t *testing.T) {
	for _, data := range [][]byte{[]byte("<html>invalid image</html>"), []byte("\x89PNG\r\n\x1a\ntruncated")} {
		if got := decodeUploadIcon(base64.StdEncoding.EncodeToString(data)); len(got) != 0 {
			t.Fatal("invalid image accepted")
		}
	}
}
