package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/openprx/fenfa/internal/config"
	"github.com/openprx/fenfa/internal/store"
)

func TestDisableBindDeviceSettingsLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, createSettings := range []bool{false, true} {
		name := "update existing settings"
		if createSettings {
			name = "create missing settings"
		}
		t.Run(name, func(t *testing.T) {
			db := openProductsAPITestDB(t)
			product, _, _ := seedProductFixture(t, db)
			if createSettings {
				if err := db.Delete(&store.SystemSettings{}, "id = ?", "default").Error; err != nil {
					t.Fatal(err)
				}
			}

			router := gin.New()
			router.GET("/settings", GetSystemSettings(db))
			router.PUT("/settings", UpdateSystemSettings(db))
			router.GET("/products/:productID", ProductDetailJSON(db, config.Default()))

			assertFlag := func(path string, want bool) {
				t.Helper()
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				var response struct {
					OK   bool `json:"ok"`
					Data struct {
						DisableBindDevice *bool `json:"disable_bind_device"`
					} `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if w.Code != http.StatusOK || !response.OK || response.Data.DisableBindDevice == nil || *response.Data.DisableBindDevice != want {
					t.Fatalf("GET %s: expected disable_bind_device=%v, got %d %s", path, want, w.Code, w.Body.String())
				}
			}

			assertFlag("/products/"+product.ID, false)
			if !createSettings {
				assertFlag("/settings", false)
			}

			enabled, disabled := true, false
			for _, step := range []struct {
				name string
				flag *bool
				want bool
			}{
				{"enable", &enabled, true},
				{"preserve when omitted", nil, true},
				{"disable again", &disabled, false},
			} {
				payload := map[string]any{
					"primary_domain": "https://download.example.com",
					"organization":   "Test Distribution",
				}
				if step.flag != nil {
					payload["disable_bind_device"] = *step.flag
				}
				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(w, req)
				var response struct {
					OK   bool                   `json:"ok"`
					Data SystemSettingsResponse `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if w.Code != http.StatusOK || !response.OK || response.Data.DisableBindDevice != step.want {
					t.Fatalf("%s: got %d %s", step.name, w.Code, w.Body.String())
				}
				assertFlag("/settings", step.want)
				assertFlag("/products/"+product.ID, step.want)
			}
		})
	}
}
