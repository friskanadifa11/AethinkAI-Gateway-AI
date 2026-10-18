package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/security"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/store"
)

// providerPayload is the console's write shape. `api_key` is write-only: it is
// accepted on create and rotate, and never returned.
type providerPayload struct {
	Name      string   `json:"name"`
	Adapter   string   `json:"adapter"`
	BaseURL   string   `json:"base_url"`
	Enabled   *bool    `json:"enabled"`
	TimeoutMS int      `json:"timeout_ms"`
	Models    []string `json:"models"`
	APIKey    string   `json:"api_key"`
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListProviders(r.Context())
	if err != nil {
		storeError(w, err, "Providers unavailable.", "Providers unavailable.")
		return
	}
	// The console shows a host, not the full URL path, and never a credential.
	for i := range list {
		list[i].BaseURL = redactHost(list[i].BaseURL)
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": list})
}

func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	var in providerPayload
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Malformed provider payload.")
		return
	}
	if msg := s.validateProvider(in); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_provider", msg)
		return
	}
	enc, fp, err := s.sealCredential(in.APIKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not seal the credential.")
		return
	}
	p, err := s.st.CreateProvider(r.Context(), toProviderInput(in, true), enc, fp)
	if err != nil {
		storeError(w, err, "Provider not found.", "A provider with that name already exists.")
		return
	}
	_ = s.st.Audit(r.Context(), user.Username, "provider.created", p.Name)
	p.BaseURL = redactHost(p.BaseURL)
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	var in providerPayload
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Malformed provider payload.")
		return
	}
	if msg := s.validateProvider(in); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_provider", msg)
		return
	}
	// An empty api_key means "keep the stored credential"; only an explicit
	// value rotates it, so a blank form field cannot wipe a working key.
	rotate := strings.TrimSpace(in.APIKey) != ""
	enc, fp, err := s.sealCredential(in.APIKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not seal the credential.")
		return
	}
	p, err := s.st.UpdateProvider(r.Context(), r.PathValue("id"), toProviderInput(in, rotate), enc, fp)
	if err != nil {
		storeError(w, err, "Provider not found.", "A provider with that name already exists.")
		return
	}
	action := "provider.updated"
	if rotate {
		action = "provider.credential_rotated"
	}
	_ = s.st.Audit(r.Context(), user.Username, action, p.Name)
	p.BaseURL = redactHost(p.BaseURL)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	id := r.PathValue("id")
	err := s.st.DeleteProvider(r.Context(), id)
	if err != nil {
		storeError(w, err, "Provider not found.",
			"This provider is still used by a route version. Replace it there first.")
		return
	}
	_ = s.st.Audit(r.Context(), user.Username, "provider.deleted", id)
	w.WriteHeader(http.StatusNoContent)
}

// handleTestProvider runs the bounded connection check. It lists models rather
// than sending a completion, so testing never spends provider tokens.
func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user, _ := currentUser(r.Context())
	ctx := r.Context()

	p, err := s.st.GetProvider(ctx, id)
	if err != nil {
		storeError(w, err, "Provider not found.", "Provider not found.")
		return
	}
	if err := ValidateProviderURL(p.BaseURL, s.cfg.AllowPrivateUpstreams); err != nil {
		_ = s.st.RecordProviderTest(ctx, id, "error", err.Error())
		writeJSON(w, http.StatusOK, map[string]any{"state": "error", "error": err.Error()})
		return
	}
	enc, err := s.st.ProviderCredential(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not read the credential.")
		return
	}
	credential, err := security.Decrypt(s.cfg.Secret, enc)
	if err != nil {
		_ = s.st.RecordProviderTest(ctx, id, "error", "stored credential cannot be decrypted")
		writeJSON(w, http.StatusOK, map[string]any{"state": "error", "error": "Stored credential cannot be decrypted with the current APP_SECRET."})
		return
	}

	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	status, snippet, err := s.up.Probe(probeCtx, adapterURL{p.BaseURL}, credential)
	state, msg := "healthy", ""
	switch {
	case err != nil:
		state, msg = "error", "Connection failed: "+err.Error()
	case status >= 200 && status < 300:
		state = "healthy"
	case status == 401 || status == 403:
		state, msg = "error", "The provider rejected the credential."
	default:
		state, msg = "error", "Provider answered with status "+http.StatusText(status)+"."
	}
	if len(snippet) > 0 && state == "error" {
		msg += " " + firstLine(snippet)
	}
	_ = s.st.RecordProviderTest(ctx, id, state, msg)
	_ = s.st.Audit(ctx, user.Username, "provider.tested", p.Name)
	writeJSON(w, http.StatusOK, map[string]any{"state": state, "error": msg, "http_status": status})
}

// adapterURL lets the probe share the Upstream client's BaseProvider contract.
type adapterURL struct{ url string }

func (a adapterURL) GetBaseURL() string { return a.url }

// validateProvider applies URL and shape checks shared by create and update.
func (s *Server) validateProvider(in providerPayload) string {
	name := strings.TrimSpace(in.Name)
	switch {
	case len(name) < 2 || len(name) > 64:
		return "Provider name must be between 2 and 64 characters."
	case in.Adapter != "" && in.Adapter != "openai-compatible":
		return "Only the openai-compatible adapter is supported in this release."
	}
	if err := ValidateProviderURL(in.BaseURL, s.cfg.AllowPrivateUpstreams); err != nil {
		return err.Error()
	}
	if in.TimeoutMS < 0 || in.TimeoutMS > 600000 {
		return "Timeout must be between 0 and 600000 milliseconds."
	}
	if len(in.Models) > 500 {
		return "A provider may declare at most 500 models."
	}
	return ""
}

func (s *Server) sealCredential(plain string) (enc string, fingerprint string, err error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", "", nil
	}
	enc, err = security.Encrypt(s.cfg.Secret, plain)
	if err != nil {
		return "", "", err
	}
	return enc, security.Fingerprint(s.cfg.Secret, plain), nil
}

func toProviderInput(in providerPayload, setCred bool) store.ProviderInput {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	adapter := in.Adapter
	if adapter == "" {
		adapter = "openai-compatible"
	}
	timeout := in.TimeoutMS
	if timeout == 0 {
		timeout = 60000
	}
	return store.ProviderInput{
		Name:        strings.TrimSpace(in.Name),
		Adapter:     adapter,
		BaseURL:     strings.TrimSpace(in.BaseURL),
		Enabled:     enabled,
		TimeoutMS:   timeout,
		Models:      in.Models,
		Credential:  in.APIKey,
		SetCredFlag: setCred,
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
