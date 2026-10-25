package store

import "testing"

func TestAutoMigrateDefaultsBuildRetentionForExistingSettings(t *testing.T) {
	db := openTestDB(t)
	if err := db.Exec(`CREATE TABLE system_settings (id text PRIMARY KEY, primary_domain text, organization text)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO system_settings (id, primary_domain, organization) VALUES ('default', 'https://dist.example.com', 'Existing')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	var settings SystemSettings
	if err := db.First(&settings, "id = ?", "default").Error; err != nil {
		t.Fatal(err)
	}
	if settings.MaxBuildsPerVariant != 50 || settings.Organization != "Existing" {
		t.Fatalf("unexpected migrated settings: %+v", settings)
	}
	if err := db.Model(&settings).Update("max_builds_per_variant", 3).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&settings, "id = ?", "default").Error; err != nil {
		t.Fatal(err)
	}
	if settings.MaxBuildsPerVariant != 3 {
		t.Fatalf("migration reset configured limit to %d", settings.MaxBuildsPerVariant)
	}
}
