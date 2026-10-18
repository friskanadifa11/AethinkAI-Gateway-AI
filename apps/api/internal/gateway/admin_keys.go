package gateway

import (
	"net/http"
	"strings"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/security"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/store"
)

type keyPayload struct {
	Label          string   `json:"label"`
	OwnerLabel     string   `json:"owner_label"`
	RPM            int      `json:"rpm"`
	MaxConcurrency int      `json:"max_concurrency"`
	TokenQuota     int64    `json:"token_quota"`
	ExpiresInDays  int      `json:"expires_in_days"`
	AllowedAliases []string `json:"allowed_aliases"`
}

type rotatePayload struct {
	GraceHours int `json:"grace_hours"`
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.st.ListClientKeys(r.Context())
	if err != nil {
		storeError(w, err, "Keys unavailable.", "Keys unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// handleCreateKey mints a key. The plaintext is returned exactly once, in this
// response; only the HMAC and a short prefix are stored.
func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	var in keyPayload
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Malformed key payload.")
		return
	}
	if msg := validateKeyInput(in); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_key", msg)
		return
	}
	rec, plaintext, err := s.mintKey(r, in)
	if err != nil {
		storeError(w, err, "Key not found.", "Could not create that key.")
		return
	}
	_ = s.st.Audit(r.Context(), user.Username, "key.created", rec.Label)
	writeJSON(w, http.StatusCreated, map[string]any{"key": rec, "token": plaintext})
}

func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	id := r.PathValue("id")
	if err := s.st.RevokeClientKey(r.Context(), id); err != nil {
		storeError(w, err, "Key not found.", "Key not found.")
		return
	}
	_ = s.st.Audit(r.Context(), user.Username, "key.revoked", id)
	w.WriteHeader(http.StatusNoContent)
}

// handleRotateKey issues a replacement with the same limits and gives the old
// key a bounded grace window, so a caller can deploy the new secret without a
// hard cutover. The old value is never recoverable, so rotation is the only
// path — there is no "show key" endpoint by design.
func (s *Server) handleRotateKey(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	id := r.PathValue("id")

	var rot rotatePayload
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &rot); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "Malformed rotation payload.")
			return
		}
	}
	grace := rot.GraceHours
	if grace == 0 {
		grace = 24
	}
	if grace < 0 || grace > 720 {
		writeError(w, http.StatusBadRequest, "invalid_grace", "Grace must be between 0 and 720 hours.")
		return
	}

	old, err := s.st.GetClientKey(r.Context(), id)
	if err != nil {
		storeError(w, err, "Key not found.", "Key not found.")
		return
	}
	next := keyPayload{
		Label:          old.Label,
		OwnerLabel:     old.OwnerLabel,
		RPM:            old.RPM,
		MaxConcurrency: old.MaxConc,
		TokenQuota:     old.TokenQuota,
		AllowedAliases: old.Allowlist,
	}
	rec, plaintext, err := s.mintKey(r, next)
	if err != nil {
		storeError(w, err, "Key not found.", "Could not rotate that key.")
		return
	}
	if err := s.st.ScheduleKeyExpiry(r.Context(), id, time.Now().Add(time.Duration(grace)*time.Hour)); err != nil {
		storeError(w, err, "Key not found.", "The new key was created but the old one could not be scheduled.")
		return
	}
	_ = s.st.Audit(r.Context(), user.Username, "key.rotated", old.Label)
	writeJSON(w, http.StatusCreated, map[string]any{
		"key":         rec,
		"token":       plaintext,
		"replaced":    old.Prefix,
		"grace_until": time.Now().Add(time.Duration(grace) * time.Hour).UTC().Format(time.RFC3339),
	})
}

// mintKey generates a key, stores its hash, and returns the one-time plaintext.
func (s *Server) mintKey(r *http.Request, in keyPayload) (models.ClientKey, string, error) {
	plaintext, prefix, err := security.NewClientKey()
	if err != nil {
		return models.ClientKey{}, "", err
	}
	input := store.KeyInput{
		Label:      strings.TrimSpace(in.Label),
		OwnerLabel: strings.TrimSpace(in.OwnerLabel),
		RPM:        in.RPM,
		MaxConc:    in.MaxConcurrency,
		TokenQuota: in.TokenQuota,
		Allowlist:  normaliseAliases(in.AllowedAliases),
		Hash:       security.HashKey(s.cfg.Secret, plaintext),
		Prefix:     prefix,
	}
	if in.ExpiresInDays > 0 {
		at := time.Now().AddDate(0, 0, in.ExpiresInDays)
		input.ExpiresAt = &at
	}
	rec, err := s.st.CreateClientKey(r.Context(), input)
	if err != nil {
		return models.ClientKey{}, "", err
	}
	return rec, plaintext, nil
}

func validateKeyInput(in keyPayload) string {
	label := strings.TrimSpace(in.Label)
	switch {
	case len(label) < 2 || len(label) > 64:
		return "Key label must be between 2 and 64 characters."
	case in.RPM < 0 || in.RPM > 100000:
		return "Requests per minute must be between 0 (unlimited) and 100000."
	case in.MaxConcurrency < 0 || in.MaxConcurrency > 1000:
		return "Max concurrency must be between 0 (unlimited) and 1000."
	case in.TokenQuota < 0:
		return "Token quota cannot be negative."
	case in.ExpiresInDays < 0 || in.ExpiresInDays > 3650:
		return "Expiry must be between 0 (never) and 3650 days."
	case len(in.AllowedAliases) > 50:
		return "At most 50 aliases may be allowlisted."
	}
	return ""
}

// normaliseAliases trims, drops blanks and de-duplicates while keeping order.
func normaliseAliases(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}
