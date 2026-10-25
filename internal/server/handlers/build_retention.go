package handlers

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gorm.io/gorm"

	"github.com/openprx/fenfa/internal/clientauth"
	s3client "github.com/openprx/fenfa/internal/s3"
	"github.com/openprx/fenfa/internal/store"
)

// Bounded lock storage serializes uploads and cleanup for the same variant.
var variantUploadLocks [64]sync.Mutex

func lockVariantUploads(variantID string) func() {
	h := fnv.New32a()
	_, _ = h.Write([]byte(variantID))
	mu := &variantUploadLocks[h.Sum32()%uint32(len(variantUploadLocks))]
	mu.Lock()
	return mu.Unlock
}

// Called only after a successful upload, while holding the variant upload lock.
func enforceBuildRetention(ctx context.Context, db *gorm.DB, settings store.SystemSettings, variantID, uploadedID string) error {
	limit := settings.MaxBuildsPerVariant
	if limit < 1 {
		limit = 50
	}

	var obsolete []store.Release
	// Always retain the build whose upload triggered this cleanup, including when
	// timestamps tie or the server clock has moved backwards.
	if err := db.Where("variant_id = ? AND id <> ?", variantID, uploadedID).
		Order("created_at desc").Order("id desc").Offset(limit - 1).Find(&obsolete).Error; err != nil {
		return err
	}

	var cleanupErrors []error
	for _, release := range obsolete {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(cleanupErrors, err)...)
		}
		// Keep the record on storage failure so the next upload can retry cleanup.
		if err := removeReleaseFiles(ctx, settings, release); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("release %s: %w", release.ID, err))
			continue
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("release_id = ?", release.ID).Delete(&store.ProvisioningProfile{}).Error; err != nil {
				return err
			}
			if err := tx.Where("scope = ? AND resource_id = ?", clientauth.DownloadScope, release.ID).
				Delete(&store.ClientAccessToken{}).Error; err != nil {
				return err
			}
			if err := tx.Where("release_id = ?", release.ID).Delete(&store.Event{}).Error; err != nil {
				return err
			}
			return tx.Delete(&release).Error
		}); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("release %s records: %w", release.ID, err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func removeReleaseFiles(ctx context.Context, settings store.SystemSettings, release store.Release) error {
	path := release.StoragePath
	if path == "" {
		return nil
	}
	if strings.HasPrefix(path, "s3://") {
		if settings.S3Endpoint == "" || settings.S3Bucket == "" || settings.S3AccessKey == "" || settings.S3SecretKey == "" {
			return fmt.Errorf("S3 credentials are required to remove stored builds")
		}
		key := strings.TrimPrefix(path, "s3://")
		client := s3client.NewClient(settings.S3Endpoint, settings.S3Bucket, settings.S3AccessKey, settings.S3SecretKey)
		if err := client.Delete(ctx, key); err != nil {
			return err
		}
		// Older uploads left a local copy at the same path as the object key.
		path = filepath.FromSlash(key)
	}
	return removeLocalReleaseFiles(path, release.ID)
}

func removeLocalReleaseFiles(path, releaseID string) error {
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return err
	}
	root, err := filepath.Abs("uploads")
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	// Only remove complete directories when they belong to this release inside
	// uploads. This also removes meta.json and supports legacy upload layouts.
	if filepath.Base(dir) == releaseID && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return os.RemoveAll(dir)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
