package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/config"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/security"
)

// hashToken derives the stored form of a session token. Sessions use a plain
// SHA-256 because the token is already 256 bits of entropy; the keyed HMAC used
// for client API keys exists because those are lower-entropy strings.
func hashToken(secret []byte, token string) string {
	sum := sha256.Sum256(append(append([]byte{}, secret...), token...))
	return hex.EncodeToString(sum[:])
}

func setSessionCookie(w http.ResponseWriter, cfg *config.Config, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !cfg.DevCookies,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, cfg *config.Config) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !cfg.DevCookies,
		MaxAge:   -1,
	})
}

// hashPassword produces the stored password hash.
func hashPassword(plain string) (string, error) {
	return security.Bcrypt(plain)
}

// checkPassword verifies a sign-in attempt.
func checkPassword(hash, plain string) bool {
	return security.BcryptCompare(hash, plain)
}

// writeJSONRaw is used where the payload is already encoded.
func writeJSONRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func encode(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
