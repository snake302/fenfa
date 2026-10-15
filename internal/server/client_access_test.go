package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openprx/fenfa/internal/clientauth"
	"github.com/openprx/fenfa/internal/config"
	"github.com/openprx/fenfa/internal/store"
	web "github.com/openprx/fenfa/internal/web"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"howett.net/plist"
)

func clientTestRouter(t *testing.T, db *gorm.DB, cfg *config.Config) *gin.Engine {
	t.Helper()
	g := gin.New()
	g.SetHTMLTemplate(template.Must(template.ParseFS(web.Templates(), "*.html")))
	RegisterRoutes(g, db, cfg)
	return g
}

func clientTestSetup(t *testing.T, password string) (*gin.Engine, *gorm.DB, *config.Config) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	cfg := config.Default()
	cfg.Auth.ClientPassword = password
	cfg.Auth.AdminTokens = []string{"test-admin-token"}
	if err := db.Model(&store.SystemSettings{}).Where("id = ?", "default").Update("primary_domain", "https://downloads.example.com").Error; err != nil {
		t.Fatal(err)
	}
	product := store.Product{ID: "prd_client", Slug: "client-product", Name: "Client Product", Published: true}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"ios", "windows"} {
		variant := store.Variant{ID: "var_" + platform, ProductID: product.ID, Platform: platform, Identifier: "com.example.client", Published: true}
		if err := db.Create(&variant).Error; err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "artifact.ipa")
		if err := os.WriteFile(path, []byte("client artifact "+platform), 0600); err != nil {
			t.Fatal(err)
		}
		release := store.Release{ID: "rel_" + platform, VariantID: variant.ID, Version: "1.0", FileExt: ".ipa", StoragePath: path}
		if err := db.Create(&release).Error; err != nil {
			t.Fatal(err)
		}
	}
	return clientTestRouter(t, db, cfg), db, cfg
}

func clientRequest(g *gin.Engine, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	if !strings.HasPrefix(path, "https://") && !strings.HasPrefix(path, "http://") {
		path = "https://downloads.example.com" + path
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("User-Agent", "iPhone Darwin CFNetwork")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)
	return w
}

func clientLogin(t *testing.T, g *gin.Engine) *http.Cookie {
	t.Helper()
	w := clientRequest(g, "POST", "/api/client/login", `{"password":"test-client-password"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != clientauth.CookieName {
		t.Fatalf("missing session cookie: %v", cookies)
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge <= 0 || cookie.Value == "test-client-password" {
		t.Fatalf("invalid cookie properties: %v", cookie)
	}
	return cookie
}

func TestClientPasswordProtectsPagesAndAPIs(t *testing.T) {
	g, _, _ := clientTestSetup(t, "test-client-password")
	for _, path := range []string{"/products/prd_client", "/products/client-product", "/apps/legacy", "/d/rel_ios", "/ios/rel_ios/manifest.plist", "/udid/profile.mobileconfig"} {
		w := clientRequest(g, "GET", path, "", nil)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" || strings.Contains(w.Body.String(), "Client Product") {
			t.Fatalf("unprotected page %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/products", "/api/products/prd_client", "/api/products/slug/client-product", "/api/apps/legacy", "/udid/status", "/icon/products/prd_client", "/icon/legacy"} {
		w := clientRequest(g, "GET", path, "", nil)
		if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "Client Product") {
			t.Fatalf("unprotected API %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/events", "/udid/callback"} {
		if w := clientRequest(g, "POST", path, `{}`, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("unprotected POST %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/", "/healthz", "/admin"} {
		if w := clientRequest(g, "GET", path, "", nil); w.Code != http.StatusOK {
			t.Fatalf("public entry %s: %d", path, w.Code)
		}
	}
}

func TestClientSessionLoginPersistenceLogoutAndAdminIsolation(t *testing.T) {
	g, db, cfg := clientTestSetup(t, "test-client-password")
	if w := clientRequest(g, "POST", "/api/client/login", `{"password":"wrong"}`, nil); w.Code != http.StatusUnauthorized || len(w.Result().Cookies()) != 0 {
		t.Fatalf("wrong password accepted: %d", w.Code)
	}
	cookie := clientLogin(t, g)
	var token store.ClientAccessToken
	if err := db.First(&token).Error; err != nil || token.TokenHash == cookie.Value {
		t.Fatalf("session must store a hash: %v", err)
	}
	for _, path := range []string{"/api/products", "/api/products/prd_client", "/products/client-product"} {
		w := clientRequest(g, "GET", path, "", cookie)
		if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatalf("authenticated %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	// A new router simulates a process restart with the same persistent database.
	restarted := clientTestRouter(t, db, cfg)
	if w := clientRequest(restarted, "GET", "/api/products", "", cookie); w.Code != http.StatusOK {
		t.Fatalf("session did not survive restart: %d", w.Code)
	}
	if w := clientRequest(g, "GET", "/admin/api/products", "", cookie); w.Code != http.StatusUnauthorized {
		t.Fatalf("client cookie granted admin access: %d", w.Code)
	}
	if w := clientRequest(g, "POST", "/api/client/login", `{"password":"test-admin-token"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("admin token accepted as client password: %d", w.Code)
	}
	if w := clientRequest(g, "POST", "/api/client/logout", `{}`, cookie); w.Code != http.StatusOK || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout failed: %d", w.Code)
	}
	if w := clientRequest(g, "GET", "/api/products", "", cookie); w.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out session still accepted: %d", w.Code)
	}
}

func TestClientSessionsExpireAndPasswordRotationRevokesAccess(t *testing.T) {
	g, db, cfg := clientTestSetup(t, "test-client-password")
	cookie := clientLogin(t, g)
	cfg.Auth.ClientPassword = "rotated-client-password"
	rotated := clientTestRouter(t, db, cfg)
	if w := clientRequest(rotated, "GET", "/api/products", "", cookie); w.Code != http.StatusUnauthorized {
		t.Fatalf("password rotation did not invalidate session: %d", w.Code)
	}
	if err := db.Model(&store.ClientAccessToken{}).Where("scope = ?", clientauth.SessionScope).Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if w := clientRequest(g, "GET", "/api/products", "", cookie); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired session still accepted: %d", w.Code)
	}
}

func TestClientDownloadGrantWorksWithoutCookiesAndIsReleaseScoped(t *testing.T) {
	g, db, cfg := clientTestSetup(t, "test-client-password")
	cookie := clientLogin(t, g)
	w := clientRequest(g, "GET", "/api/products/prd_client", "", cookie)
	var response struct {
		Data struct {
			Variants []struct {
				Platform string `json:"platform"`
				Releases []struct {
					Actions map[string]string `json:"actions"`
				} `json:"releases"`
			} `json:"variants"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var actions map[string]string
	for _, variant := range response.Data.Variants {
		if variant.Platform == "ios" {
			actions = variant.Releases[0].Actions
		}
	}
	manifestURL, err := url.Parse(actions["ios_manifest"])
	if err != nil || manifestURL.Query().Get("access_token") == "" {
		t.Fatalf("missing installer capability: %v", actions)
	}
	installURL, err := url.Parse(actions["ios_install"])
	if err != nil || installURL.Query().Get("url") != manifestURL.String() {
		t.Fatalf("invalid installation URL: %v", actions)
	}
	w = clientRequest(g, "GET", manifestURL.String(), "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("manifest without Safari cookies: %d %s", w.Code, w.Body.String())
	}
	var manifest map[string]interface{}
	if _, err := plist.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	item := manifest["items"].([]interface{})[0].(map[string]interface{})
	asset := item["assets"].([]interface{})[0].(map[string]interface{})
	downloadURL := asset["url"].(string)
	if w := clientRequest(g, "GET", downloadURL, "", nil); w.Code != http.StatusOK || w.Body.String() != "client artifact ios" {
		t.Fatalf("IPA without Safari cookies: %d %s", w.Code, w.Body.String())
	}
	rangeRequest := httptest.NewRequest("GET", downloadURL, nil)
	rangeRequest.Header.Set("Range", "bytes=0-5")
	rangeResponse := httptest.NewRecorder()
	g.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent || rangeResponse.Body.String() != "client" || !strings.Contains(rangeResponse.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("authorized ranged download failed: %d %s", rangeResponse.Code, rangeResponse.Body.String())
	}
	grant := manifestURL.Query().Get("access_token")
	for _, path := range []string{"/d/rel_windows?access_token=" + grant, "/d/rel_ios?access_token=" + grant + "0"} {
		if w := clientRequest(g, "GET", path, "", nil); w.Code != http.StatusSeeOther {
			t.Fatalf("invalid grant accepted at %s: %d", path, w.Code)
		}
	}
	if w := clientRequest(g, "GET", "/api/products?access_token="+grant, "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("download grant permitted catalog access: %d", w.Code)
	}
	changed := *cfg
	changed.Auth.ClientPassword = "rotated-client-password"
	if w := clientRequest(clientTestRouter(t, db, &changed), "GET", downloadURL, "", nil); w.Code != http.StatusSeeOther {
		t.Fatalf("password rotation did not invalidate download grant: %d", w.Code)
	}
	if err := db.Model(&store.ClientAccessToken{}).Where("scope = ?", clientauth.DownloadScope).Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if w := clientRequest(g, "GET", downloadURL, "", nil); w.Code != http.StatusSeeOther {
		t.Fatalf("expired download grant accepted: %d", w.Code)
	}
}

func TestClientUDIDCallbackUsesAuthorizedProfileNonce(t *testing.T) {
	g, _, _ := clientTestSetup(t, "test-client-password")
	cookie := clientLogin(t, g)
	w := clientRequest(g, "GET", "/udid/profile.mobileconfig?variant=var_ios", "", cookie)
	var profile map[string]interface{}
	if w.Code != http.StatusOK {
		t.Fatalf("profile: %d %s", w.Code, w.Body.String())
	}
	if _, err := plist.Unmarshal(w.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	callbackURL := profile["PayloadContent"].(map[string]interface{})["URL"].(string)
	body := `<plist><dict><key>UDID</key><string>00000000-1111111111111111</string></dict></plist>`
	w = clientRequest(g, "POST", callbackURL, body, nil)
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("system callback without Safari session failed: %d %s", w.Code, w.Body.String())
	}
}

func TestClientPasswordOptionalAndLoginRateLimited(t *testing.T) {
	t.Run("optional password", func(t *testing.T) {
		g, _, _ := clientTestSetup(t, "")
		for _, path := range []string{"/api/products", "/products/prd_client", "/d/rel_ios"} {
			if w := clientRequest(g, "GET", path, "", nil); w.Code != http.StatusOK {
				t.Fatalf("public mode %s: %d", path, w.Code)
			}
		}
	})
	t.Run("rate limited", func(t *testing.T) {
		g, _, _ := clientTestSetup(t, "test-client-password")
		for i := 0; i < 10; i++ {
			if w := clientRequest(g, "POST", "/api/client/login", `{"password":"wrong"}`, nil); w.Code != http.StatusUnauthorized {
				t.Fatalf("attempt %d: %d", i, w.Code)
			}
		}
		w := clientRequest(g, "POST", "/api/client/login", `{"password":"wrong"}`, nil)
		if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
			t.Fatalf("login attempts were not limited: %d", w.Code)
		}
	})
}

func TestClientLoginRejectsMalformedAndOversizedRequests(t *testing.T) {
	g, _, _ := clientTestSetup(t, "test-client-password")
	for _, body := range []string{
		`{"password":`,
		`{"password":"` + strings.Repeat("x", 5000) + `"}`,
		`{"password":"test-client-password"}` + strings.Repeat(" ", 5000),
		`{"password":"test-client-password"} {}`,
	} {
		w := clientRequest(g, "POST", "/api/client/login", body, nil)
		if w.Code != http.StatusBadRequest || len(w.Result().Cookies()) != 0 {
			t.Fatalf("invalid request created a session: %d", w.Code)
		}
	}
	for _, path := range []string{"/api/client/login", "/api/client/logout"} {
		req := httptest.NewRequest("POST", "https://downloads.example.com"+path, strings.NewReader("password=test-client-password"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		g.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest || len(w.Result().Cookies()) != 0 {
			t.Fatalf("cross-site form accepted at %s: %d", path, w.Code)
		}
	}
}

func TestClientSessionCookieSecureBehindHTTPSProxy(t *testing.T) {
	g, _, _ := clientTestSetup(t, "test-client-password")
	req := httptest.NewRequest("POST", "http://downloads.example.com/api/client/login", strings.NewReader(`{"password":"test-client-password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK || len(w.Result().Cookies()) != 1 || !w.Result().Cookies()[0].Secure {
		t.Fatalf("proxy login did not set a Secure cookie: %d", w.Code)
	}
}
