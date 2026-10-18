package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/security"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/store"
)

// authFailure describes why a key was refused. These map to OpenAI-shaped
// errors so an SDK surfaces the reason without special handling.
type authFailure struct {
	Status  int
	Code    string
	Message string
}

func (s *Server) authenticate(r *http.Request) (store.AuthKey, *authFailure) {
	token := bearerToken(r)
	if token == "" {
		return store.AuthKey{}, &authFailure{
			Status: http.StatusUnauthorized, Code: "missing_api_key",
			Message: "Provide your gateway key as a Bearer token.",
		}
	}
	// The prefix is only a lookup hint; the hash comparison below is the check.
	if !strings.HasPrefix(token, "atk_") || len(token) < 12 {
		return store.AuthKey{}, &authFailure{
			Status: http.StatusUnauthorized, Code: "invalid_api_key",
			Message: "That API key is not valid.",
		}
	}
	candidates, err := s.st.KeyByPrefix(r.Context(), token[:12])
	if err != nil {
		return store.AuthKey{}, &authFailure{
			Status: http.StatusInternalServerError, Code: "internal",
			Message: "Key verification is unavailable right now.",
		}
	}
	want := security.HashKey(s.cfg.Secret, token)
	var match *store.AuthKey
	for i := range candidates {
		// Constant-time compare so a near-miss cannot be found by timing.
		if subtle.ConstantTimeCompare([]byte(candidates[i].Hash), []byte(want)) == 1 {
			match = &candidates[i]
			break
		}
	}
	if match == nil {
		return store.AuthKey{}, &authFailure{
			Status: http.StatusUnauthorized, Code: "invalid_api_key",
			Message: "That API key is not valid.",
		}
	}
	now := time.Now()
	switch {
	case !match.Enabled || match.RevokedAt != nil:
		return store.AuthKey{}, &authFailure{
			Status: http.StatusUnauthorized, Code: "key_revoked",
			Message: "That API key has been revoked.",
		}
	case match.ExpiresAt != nil && now.After(*match.ExpiresAt):
		return store.AuthKey{}, &authFailure{
			Status: http.StatusUnauthorized, Code: "key_expired",
			Message: "That API key has expired.",
		}
	case match.TokenQuota > 0 && match.TokensUsed >= match.TokenQuota:
		return store.AuthKey{}, &authFailure{
			Status: http.StatusTooManyRequests, Code: "quota_exhausted",
			Message: "The token quota for this API key is exhausted.",
		}
	}
	return *match, nil
}

// allowed reports whether the key may use the alias. An empty allowlist means
// every published alias.
func allowed(key store.AuthKey, alias string) bool {
	if len(key.Allowlist) == 0 {
		return true
	}
	for _, a := range key.Allowlist {
		if a == alias {
			return true
		}
	}
	return false
}

func bearerToken(r *http.Request) string {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if raw == "" {
		return ""
	}
	if len(raw) > 7 && strings.EqualFold(raw[:7], "bearer ") {
		return strings.TrimSpace(raw[7:])
	}
	return ""
}

// openAIError writes the error envelope the OpenAI SDKs expect.
func openAIError(w http.ResponseWriter, status int, code, message, errType string) {
	if errType == "" {
		errType = "invalid_request_error"
	}
	body := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errType,
			"code":    code,
		},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// handleModels lists the aliases a key may call, in OpenAI's /v1/models shape.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	key, fail := s.authenticate(r)
	if fail != nil {
		openAIError(w, fail.Status, fail.Code, fail.Message, "authentication_error")
		return
	}
	aliases, err := s.st.PublishedAliases(r.Context())
	if err != nil {
		openAIError(w, http.StatusInternalServerError, "internal", "Could not list models.", "api_error")
		return
	}
	data := []map[string]any{}
	for _, a := range aliases {
		if !allowed(key, a) {
			continue
		}
		data = append(data, map[string]any{
			"id":       a,
			"object":   "model",
			"created":  0,
			"owned_by": "aethinkai-gateway",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}
