package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/openprx/fenfa/internal/clientauth"
	"github.com/openprx/fenfa/internal/config"
	"github.com/openprx/fenfa/internal/store"
)

func seedRetentionRelease(t *testing.T, db *gorm.DB, variant store.Variant, id string, createdAt time.Time) store.Release {
	t.Helper()
	path := filepath.Join("uploads", variant.ProductID, variant.ID, id, "app.msi")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{path, filepath.Join(filepath.Dir(path), "meta.json")} {
		if err := os.WriteFile(filename, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	release := store.Release{ID: id, VariantID: variant.ID, Build: 9999, StoragePath: path, CreatedAt: createdAt}
	for _, record := range []any{
		&release,
		&store.ProvisioningProfile{ID: "profile_" + id, ReleaseID: id},
		&store.Event{Type: "upload", VariantID: variant.ID, ReleaseID: id},
		&store.ClientAccessToken{TokenHash: "grant_" + id, Scope: clientauth.DownloadScope, ResourceID: id, ExpiresAt: time.Now().Add(time.Hour)},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	return release
}

func assertRetentionRelease(t *testing.T, db *gorm.DB, release store.Release, exists bool) {
	t.Helper()
	want := int64(0)
	if exists {
		want = 1
	}
	for _, check := range []struct {
		model any
		query string
	}{
		{&store.Release{}, "id = ?"},
		{&store.ProvisioningProfile{}, "release_id = ?"},
		{&store.Event{}, "release_id = ?"},
		{&store.ClientAccessToken{}, "resource_id = ?"},
	} {
		var count int64
		if err := db.Model(check.model).Where(check.query, release.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("%T for %s: got %d records, want %d", check.model, release.ID, count, want)
		}
	}
	_, err := os.Stat(filepath.Dir(release.StoragePath))
	if exists && err != nil || !exists && !os.IsNotExist(err) {
		t.Fatalf("release %s directory: exists=%v, stat error=%v", release.ID, exists, err)
	}
}

func TestUploadEnforcesBuildRetentionPerVariant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"/upload", "/smart-upload"} {
		for _, limit := range []int{1, 2, 50} {
			t.Run(fmt.Sprintf("%s/limit_%d", endpoint, limit), func(t *testing.T) {
				withUploadWorkdir(t)
				db := openHandlerTestDB(t)
				variant := seedVariant(t, db, "windows", "com.example.retention")
				if limit != 50 {
					if err := db.Model(&store.SystemSettings{}).Where("id = ?", "default").Update("max_builds_per_variant", limit).Error; err != nil {
						t.Fatal(err)
					}
				}
				sibling := variant
				sibling.ID = "var_sibling"
				if err := db.Create(&sibling).Error; err != nil {
					t.Fatal(err)
				}
				createdAt := time.Now().Add(-time.Hour)
				other := seedRetentionRelease(t, db, sibling, "rel_other", createdAt)
				var previous []store.Release
				for i := 0; i < limit+2; i++ {
					// Equal timestamps exercise deterministic ordering by release ID.
					previous = append(previous, seedRetentionRelease(t, db, variant, fmt.Sprintf("rel_old_%03d", i), createdAt))
				}
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = makeMultipartRequest(t, endpoint, map[string]string{"variant_id": variant.ID, "version": "0.1", "build": "1"}, "demo.msi", []byte("MSI-DEMO"))
				handler := Upload(db, config.Default())
				if endpoint == "/smart-upload" {
					handler = SmartUpload(db, config.Default())
				}
				handler(c)
				if w.Code != http.StatusCreated {
					t.Fatalf("upload: %d %s", w.Code, w.Body.String())
				}
				newID := decodeJSON(t, w)["data"].(map[string]any)["release"].(map[string]any)["id"].(string)
				var current store.Release
				if err := db.First(&current, "id = ?", newID).Error; err != nil {
					t.Fatalf("new build was not retained: %v", err)
				}
				if _, err := os.Stat(current.StoragePath); err != nil {
					t.Fatalf("new file was not retained: %v", err)
				}
				var count int64
				db.Model(&store.Release{}).Where("variant_id = ?", variant.ID).Count(&count)
				if count != int64(limit) {
					t.Fatalf("got %d builds, want %d", count, limit)
				}
				for i, release := range previous {
					assertRetentionRelease(t, db, release, i >= len(previous)-(limit-1))
				}
				assertRetentionRelease(t, db, other, true)
			})
		}
	}
}

func TestFailedUploadDoesNotPruneBuilds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"/upload", "/smart-upload"} {
		t.Run(endpoint, func(t *testing.T) {
			withUploadWorkdir(t)
			db := openHandlerTestDB(t)
			variant := seedVariant(t, db, "windows", "com.example.failed")
			db.Model(&store.SystemSettings{}).Where("id = ?", "default").Update("max_builds_per_variant", 1)
			old := seedRetentionRelease(t, db, variant, "rel_old", time.Now().Add(-time.Hour))
			if err := db.Exec("CREATE TRIGGER fail_new_release BEFORE INSERT ON releases BEGIN SELECT RAISE(FAIL, 'simulated insert failure'); END").Error; err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = makeMultipartRequest(t, endpoint, map[string]string{"variant_id": variant.ID}, "demo.msi", []byte("MSI-DEMO"))
			handler := Upload(db, config.Default())
			if endpoint == "/smart-upload" {
				handler = SmartUpload(db, config.Default())
			}
			handler(c)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("upload should fail: %d %s", w.Code, w.Body.String())
			}
			assertRetentionRelease(t, db, old, true)
			dirs, err := os.ReadDir(filepath.Dir(filepath.Dir(old.StoragePath)))
			if err != nil || len(dirs) != 1 || dirs[0].Name() != old.ID {
				t.Fatalf("failed upload left staging files: %v, %v", dirs, err)
			}
		})
	}
}

func TestConcurrentUploadsRespectBuildRetention(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withUploadWorkdir(t)
	db := openHandlerTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	variant := seedVariant(t, db, "windows", "com.example.concurrent")
	db.Model(&store.SystemSettings{}).Where("id = ?", "default").Update("max_builds_per_variant", 2)
	router := gin.New()
	router.POST("/upload", Upload(db, config.Default()))
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		req := makeMultipartRequest(t, "/upload", map[string]string{"variant_id": variant.ID, "build": fmt.Sprint(i)}, "demo.msi", []byte("MSI-DEMO"))
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			responses <- w
		}()
	}
	wg.Wait()
	close(responses)
	for w := range responses {
		if w.Code != http.StatusCreated {
			t.Fatalf("concurrent upload: %d %s", w.Code, w.Body.String())
		}
	}
	var retained []store.Release
	if err := db.Where("variant_id = ?", variant.ID).Find(&retained).Error; err != nil {
		t.Fatal(err)
	}
	dirs, err := os.ReadDir(filepath.Join("uploads", variant.ProductID, variant.ID))
	if err != nil || len(retained) != 2 || len(dirs) != 2 {
		t.Fatalf("expected 2 records and directories, got %d records, %d directories, error=%v", len(retained), len(dirs), err)
	}
	for _, release := range retained {
		if _, err := os.Stat(release.StoragePath); err != nil {
			t.Fatal(err)
		}
	}
}

func TestS3RetentionRetriesFailedDeletionAndRemovesLocalCopies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withUploadWorkdir(t)
	db := openHandlerTestDB(t)
	variant := seedVariant(t, db, "windows", "com.example.s3")
	old := seedRetentionRelease(t, db, variant, "rel_old", time.Now().Add(-time.Hour))
	var mu sync.Mutex
	denyDelete, denyUpload := true, false
	objects := map[string]bool{"/bucket/" + filepath.ToSlash(old.StoragePath): true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			if denyUpload {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = io.Copy(io.Discard, r.Body)
			objects[r.URL.Path] = true
		case http.MethodDelete:
			if denyDelete {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			delete(objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected S3 request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	if err := db.Model(&store.SystemSettings{}).Where("id = ?", "default").Updates(map[string]any{
		"max_builds_per_variant": 1, "storage_type": "s3", "s3_endpoint": server.URL,
		"s3_bucket": "bucket", "s3_access_key": "test-key", "s3_secret_key": "test-secret",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&old).Update("storage_path", "s3://"+filepath.ToSlash(old.StoragePath)).Error; err != nil {
		t.Fatal(err)
	}
	for step := 0; step < 3; step++ {
		mu.Lock()
		denyUpload = step == 0
		denyDelete = step != 2
		mu.Unlock()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = makeMultipartRequest(t, "/upload", map[string]string{"variant_id": variant.ID}, "demo.msi", []byte("MSI-DEMO"))
		Upload(db, config.Default())(c)
		var count int64
		db.Model(&store.Release{}).Where("variant_id = ?", variant.ID).Count(&count)
		if step == 0 {
			if w.Code != http.StatusInternalServerError || count != 1 {
				t.Fatalf("failed S3 upload pruned builds: %d %s, count=%d", w.Code, w.Body.String(), count)
			}
			continue
		}
		if w.Code != http.StatusCreated {
			t.Fatalf("S3 upload: %d %s", w.Code, w.Body.String())
		}
		if step == 1 {
			if count != 2 {
				t.Fatalf("failed deletion lost the retry record: count=%d", count)
			}
			continue
		}
		if count != 1 {
			t.Fatalf("cleanup retry did not restore the limit: %d", count)
		}
		mu.Lock()
		objectCount := len(objects)
		mu.Unlock()
		if objectCount != 1 {
			t.Fatalf("expected only the newest S3 object, got %d", objectCount)
		}
	}
	dirs, err := os.ReadDir(filepath.Join("uploads", variant.ProductID, variant.ID))
	if err != nil || len(dirs) != 0 {
		t.Fatalf("S3 uploads left local copies: %v, %v", dirs, err)
	}
	// The old file is gone along with all build-specific database records.
	old.StoragePath = strings.TrimPrefix(old.StoragePath, "s3://")
	assertRetentionRelease(t, db, old, false)
}

func TestRetentionAlwaysKeepsTriggeringUpload(t *testing.T) {
	withUploadWorkdir(t)
	db := openHandlerTestDB(t)
	variant := seedVariant(t, db, "windows", "com.example.clock")
	future := seedRetentionRelease(t, db, variant, "rel_future", time.Now().Add(time.Hour))
	current := seedRetentionRelease(t, db, variant, "rel_current", time.Now())
	if err := enforceBuildRetention(context.Background(), db, store.SystemSettings{MaxBuildsPerVariant: 1}, variant.ID, current.ID); err != nil {
		t.Fatal(err)
	}
	assertRetentionRelease(t, db, future, false)
	assertRetentionRelease(t, db, current, true)
}

func TestS3UploadKeepsProfileAfterRemovingStagingFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withUploadWorkdir(t)
	db := openHandlerTestDB(t)
	variant := seedVariant(t, db, "ios", "com.example.profile")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := db.Model(&store.SystemSettings{}).Where("id = ?", "default").Updates(map[string]any{
		"max_builds_per_variant": 1, "storage_type": "s3", "s3_endpoint": server.URL,
		"s3_bucket": "bucket", "s3_access_key": "test-key", "s3_secret_key": "test-secret",
	}).Error; err != nil {
		t.Fatal(err)
	}
	archive := iconArchive(t, map[string][]byte{
		"Payload/Demo.app/embedded.mobileprovision": []byte(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>UUID</key><string>profile-uuid</string></dict></plist>`),
	})
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = makeMultipartRequest(t, "/upload", map[string]string{"variant_id": variant.ID}, "demo.ipa", archive)
		Upload(db, config.Default())(c)
		if w.Code != http.StatusCreated {
			t.Fatalf("upload: %d %s", w.Code, w.Body.String())
		}
		newID := decodeJSON(t, w)["data"].(map[string]any)["release"].(map[string]any)["id"].(string)
		var profiles []store.ProvisioningProfile
		if err := db.Find(&profiles).Error; err != nil {
			t.Fatal(err)
		}
		if len(profiles) != 1 || profiles[0].ReleaseID != newID || profiles[0].UUID != "profile-uuid" {
			t.Fatalf("expected only the newest build's profile: %+v", profiles)
		}
		dirs, err := os.ReadDir(filepath.Join("uploads", variant.ProductID, variant.ID))
		if err != nil || len(dirs) != 0 {
			t.Fatalf("S3 staging files remain: %v, %v", dirs, err)
		}
	}
}
