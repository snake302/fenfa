package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClientPasswordEnvironmentOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"auth":{"client_password":"config-password","admin_tokens":["test-admin"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FENFA_ADMIN_TOKEN", "test-admin")
	t.Setenv("FENFA_CLIENT_PASSWORD", "environment-password")
	cfg, err := Load(path)
	if err != nil || cfg.Auth.ClientPassword != "environment-password" || cfg.Auth.AdminTokens[0] != "test-admin" {
		t.Fatalf("client password must be independent of admin tokens: %v", err)
	}
	t.Setenv("FENFA_CLIENT_PASSWORD", "")
	cfg, err = Load(path)
	if err != nil || cfg.Auth.ClientPassword != "" {
		t.Fatalf("explicit empty environment must disable client authentication: %v", err)
	}
}
