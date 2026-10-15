package handlers

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/avast/apkparser"
	"gorm.io/gorm"
	"howett.net/plist"

	"github.com/openprx/fenfa/internal/store"
)

const maxIconBytes = 8 << 20
const maxIconResourceBytes = 64 << 20

// Only read archive entries: extracting an icon must never rewrite a signed build.
func extractBuildIcon(filename, platform string) (icon string) {
	if platform != "ios" && platform != "android" {
		return ""
	}
	// Third-party resource decoders can panic on malformed input. Icons are optional.
	defer func() {
		if recover() != nil {
			icon = ""
		}
	}()
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return ""
	}
	defer zr.Close()
	// Exclude oversized entries before passing the archive to the existing decoders.
	files := make([]*zip.File, 0, len(zr.File))
	for _, f := range zr.File {
		limit := uint64(maxIconBytes)
		if filepath.Base(f.Name) == "Assets.car" || f.Name == "resources.arsc" {
			limit = maxIconResourceBytes
		}
		if f.UncompressedSize64 <= limit {
			files = append(files, f)
		}
	}
	zr.File = files
	if platform == "android" {
		if ref := apkIconReference(&zr.Reader); ref != "" {
			// Resource tables may resolve the reference directly to an archive path.
			if icon := readZipImageBase64(&zr.Reader, ref); icon != "" {
				return icon
			}
			if icon, ok := extractAPKIconCandidates(zr, ref); ok {
				return icon
			}
		}
		return extractAPKIconFromZip(zr)
	}
	for _, f := range zr.File {
		parts := strings.Split(f.Name, "/")
		if len(parts) != 3 || parts[0] != "Payload" || !strings.HasSuffix(parts[1], ".app") || parts[2] != "Info.plist" {
			continue
		}
		data := readIconEntry(f, maxIconBytes)
		var info map[string]interface{}
		if _, err := plist.Unmarshal(data, &info); err != nil {
			return ""
		}
		appDir := "Payload/" + parts[1]
		if icon := extractIPAIcon(&zr.Reader, appDir, info); icon != "" {
			return icon
		}
		return extractIPAIconFromAssetsCar(&zr.Reader, appDir, info)
	}
	return ""
}

func readIconEntry(f *zip.File, limit int64) []byte {
	if f.UncompressedSize64 > uint64(limit) {
		return nil
	}
	r, err := f.Open()
	if err != nil {
		return nil
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil
	}
	return data
}

func apkIconReference(zr *zip.Reader) (ref string) {
	defer func() {
		if recover() != nil {
			ref = ""
		}
	}()
	var manifest, resources []byte
	for _, f := range zr.File {
		switch f.Name {
		case "AndroidManifest.xml":
			manifest = readIconEntry(f, maxIconBytes)
		case "resources.arsc":
			resources = readIconEntry(f, maxIconResourceBytes)
		}
	}
	if len(manifest) == 0 {
		return ""
	}
	if !bytes.HasPrefix(bytes.TrimSpace(manifest), []byte("<")) {
		var table *apkparser.ResourceTable
		if len(resources) > 0 {
			table, _ = apkparser.ParseResourceTable(bytes.NewReader(resources))
		}
		var decoded bytes.Buffer
		if err := apkparser.ParseXml(bytes.NewReader(manifest), xml.NewEncoder(&decoded), table); err != nil {
			return ""
		}
		manifest = decoded.Bytes()
	}
	decoder := xml.NewDecoder(bytes.NewReader(manifest))
	for {
		tok, err := decoder.Token()
		if err != nil {
			return ""
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local == "application" {
			for _, attr := range start.Attr {
				if attr.Name.Local == "icon" {
					return attr.Value
				}
				if attr.Name.Local == "roundIcon" {
					ref = attr.Value
				}
			}
			return ref
		}
	}
}

func saveUploadIcon(db *gorm.DB, p uploadParams, buildPath string) {
	data := decodeUploadIcon(p.IconBase64)
	if len(data) == 0 {
		data = decodeUploadIcon(extractBuildIcon(buildPath, p.Platform))
	}
	if len(data) == 0 {
		return
	}
	iconPath := filepath.Join("uploads", p.ProductID, "icon.png")
	// Rename a completed file so concurrent readers cannot see a partial PNG.
	f, err := os.CreateTemp(filepath.Dir(iconPath), ".icon-*.png")
	if err != nil {
		log.Printf("[upload] cannot save product icon: %v", err)
		return
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		log.Printf("[upload] cannot write product icon: write=%v close=%v", writeErr, closeErr)
		return
	}
	if err := os.Rename(f.Name(), iconPath); err != nil {
		log.Printf("[upload] cannot replace product icon: %v", err)
		return
	}
	iconPath = filepath.ToSlash(iconPath)
	if err := db.Model(&store.Product{}).Where("id = ?", p.ProductID).Update("icon_path", iconPath).Error; err != nil {
		log.Printf("[upload] cannot update product icon: %v", err)
	}
	if p.LegacyAppID != "" {
		if err := db.Model(&store.App{}).Where("id = ?", p.LegacyAppID).Update("icon_path", iconPath).Error; err != nil {
			log.Printf("[upload] cannot update legacy app icon: %v", err)
		}
	}
}

func decodeUploadIcon(encoded string) []byte {
	if i := strings.Index(encoded, "base64,"); i >= 0 {
		encoded = encoded[i+7:]
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(maxIconBytes) {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return nil
	}
	// Validate and normalize supplied and extracted icons to a browser-readable PNG.
	normalized := imageToBase64(data)
	if normalized == "" {
		return nil
	}
	data, _ = base64.StdEncoding.DecodeString(strings.TrimPrefix(normalized, "data:image/png;base64,"))
	return data
}
