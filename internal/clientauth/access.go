package clientauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"time"

	"github.com/openprx/fenfa/internal/store"
	"gorm.io/gorm"
)

const (
	CookieName       = "fenfa_client_session"
	SessionLifetime  = 7 * 24 * time.Hour
	DownloadLifetime = time.Hour
	SessionScope     = "session"
	DownloadScope    = "download"
)

type Access struct {
	db       *gorm.DB
	password string
}

func New(db *gorm.DB, password string) *Access {
	return &Access{db: db, password: password}
}

func (a *Access) Enabled() bool { return a.password != "" }

// Hashing with the configured password invalidates outstanding tokens after rotation.
func (a *Access) tokenHash(token string) string {
	mac := hmac.New(sha256.New, []byte(a.password))
	mac.Write([]byte("fenfa-client-access-v1:" + token))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *Access) Issue(scope, resourceID string, lifetime time.Duration) (string, error) {
	if !a.Enabled() {
		return "", nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	now := time.Now()
	if scope == SessionScope {
		// Expired grants and sessions do not need to survive subsequent logins.
		a.db.Where("expires_at <= ?", now).Delete(&store.ClientAccessToken{})
	}
	record := store.ClientAccessToken{
		TokenHash: a.tokenHash(token), Scope: scope, ResourceID: resourceID, ExpiresAt: now.Add(lifetime),
	}
	if err := a.db.Create(&record).Error; err != nil {
		return "", err
	}
	return token, nil
}

func (a *Access) Valid(token, scope, resourceID string) bool {
	if len(token) != 64 {
		return false
	}
	var count int64
	err := a.db.Model(&store.ClientAccessToken{}).
		Where("token_hash = ? AND scope = ? AND resource_id = ? AND expires_at > ?", a.tokenHash(token), scope, resourceID, time.Now()).
		Count(&count).Error
	return err == nil && count == 1
}

func (a *Access) RevokeSession(token string) error {
	return a.db.Where("token_hash = ? AND scope = ?", a.tokenHash(token), SessionScope).Delete(&store.ClientAccessToken{}).Error
}

// WithToken adds a release-scoped capability for downloads outside the browser session.
func WithToken(rawURL, token string) string {
	if token == "" {
		return rawURL
	}
	separator := "?"
	if parsed, err := url.Parse(rawURL); err == nil && parsed.RawQuery != "" {
		separator = "&"
	}
	return rawURL + separator + "access_token=" + url.QueryEscape(token)
}
