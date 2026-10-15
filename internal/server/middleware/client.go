package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openprx/fenfa/internal/clientauth"
	"github.com/openprx/fenfa/internal/config"
	"github.com/openprx/fenfa/internal/store"
	"gorm.io/gorm"
)

type loginWindow struct {
	start time.Time
	count int
}

type ClientAccess struct {
	access   *clientauth.Access
	db       *gorm.DB
	password [32]byte
	mu       sync.Mutex
	global   loginWindow
	attempts map[string]loginWindow
}

func NewClientAccess(db *gorm.DB, cfg *config.Config) *ClientAccess {
	return &ClientAccess{
		access: clientauth.New(db, cfg.Auth.ClientPassword), db: db,
		password: sha256.Sum256([]byte(cfg.Auth.ClientPassword)), attempts: make(map[string]loginWindow),
	}
}

func clientNoStore(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Referrer-Policy", "no-referrer")
}

func (a *ClientAccess) authenticated(c *gin.Context) bool {
	if !a.access.Enabled() {
		return true
	}
	token, err := c.Cookie(clientauth.CookieName)
	return err == nil && a.access.Valid(token, clientauth.SessionScope, "")
}

func (a *ClientAccess) RequirePage() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientNoStore(c)
		if !a.authenticated(c) {
			c.Redirect(http.StatusSeeOther, "/")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (a *ClientAccess) RequireAPI() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientNoStore(c)
		if !a.authenticated(c) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error": gin.H{"code": "CLIENT_LOGIN_REQUIRED", "message": "client login required"}})
			return
		}
		c.Next()
	}
}

func (a *ClientAccess) RequireDownload() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientNoStore(c)
		if !a.authenticated(c) && !a.access.Valid(c.Query("access_token"), clientauth.DownloadScope, c.Param("releaseID")) {
			c.Redirect(http.StatusSeeOther, "/")
			c.Abort()
			return
		}
		c.Next()
	}
}

// iOS sends the callback outside Safari, so authorization follows the issued nonce.
func (a *ClientAccess) RequireUDIDCallback() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientNoStore(c)
		if a.access.Enabled() {
			var nonce store.UDIDNonce
			if c.Query("nonce") == "" || a.db.Where("nonce = ? AND (expires_at > ? OR used = ?)", c.Query("nonce"), time.Now(), true).First(&nonce).Error != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error": gin.H{"code": "INVALID_NONCE", "message": "invalid or expired nonce"}})
				return
			}
		}
		c.Next()
	}
}

func (a *ClientAccess) Session(c *gin.Context) {
	clientNoStore(c)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{"enabled": a.access.Enabled(), "authenticated": a.authenticated(c)}})
}

func (a *ClientAccess) allowLogin(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if now.Sub(a.global.start) >= time.Minute {
		a.global = loginWindow{start: now}
		a.attempts = make(map[string]loginWindow)
	}
	window := a.attempts[ip]
	if now.Sub(window.start) >= time.Minute {
		window = loginWindow{start: now}
	}
	if a.global.count >= 100 || window.count >= 10 {
		return false
	}
	a.global.count++
	window.count++
	a.attempts[ip] = window
	return true
}

func (a *ClientAccess) setCookie(c *gin.Context, token string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
	c.SetCookie(clientauth.CookieName, token, maxAge, "/", "", secure, true)
}

func (a *ClientAccess) Login(c *gin.Context) {
	clientNoStore(c)
	if !a.access.Enabled() {
		a.Session(c)
		return
	}
	if !a.allowLogin(c.ClientIP()) {
		c.Header("Retry-After", "60")
		c.JSON(http.StatusTooManyRequests, gin.H{"ok": false, "error": gin.H{"code": "RATE_LIMITED", "message": "too many login attempts"}})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var request struct {
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	var trailing any
	if c.ContentType() != "application/json" || decoder.Decode(&request) != nil || decoder.Decode(&trailing) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "BAD_REQUEST", "message": "expected a JSON password"}})
		return
	}
	provided := sha256.Sum256([]byte(request.Password))
	if subtle.ConstantTimeCompare(provided[:], a.password[:]) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": gin.H{"code": "INVALID_PASSWORD", "message": "invalid password"}})
		return
	}
	token, err := a.access.Issue(clientauth.SessionScope, "", clientauth.SessionLifetime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "SESSION_ERROR", "message": "failed to create session"}})
		return
	}
	if old, err := c.Cookie(clientauth.CookieName); err == nil {
		_ = a.access.RevokeSession(old)
	}
	a.setCookie(c, token, int(clientauth.SessionLifetime.Seconds()))
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{"enabled": true, "authenticated": true}})
}

func (a *ClientAccess) Logout(c *gin.Context) {
	clientNoStore(c)
	if c.ContentType() != "application/json" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "BAD_REQUEST", "message": "expected a JSON request"}})
		return
	}
	if token, err := c.Cookie(clientauth.CookieName); err == nil {
		if err := a.access.RevokeSession(token); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "SESSION_ERROR", "message": "failed to end session"}})
			return
		}
	}
	a.setCookie(c, "", -1)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
