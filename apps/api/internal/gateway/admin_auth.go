package gateway

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/security"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/store"
)

// handleSetupState lets the console decide between the first-run wizard and the
// sign-in form without probing an authenticated endpoint.
func (s *Server) handleSetupState(w http.ResponseWriter, r *http.Request) {
	n, err := s.st.CountAdmins(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not read the operator table.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"needs_bootstrap":      n == 0,
		"bootstrap_configured": s.cfg.BootstrapToken != "",
	})
}

type bootstrapPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

// handleBootstrap creates the single first operator. It is available only while
// no operator exists and only with the one-use token from the environment, so a
// freshly exposed installation is never open.
func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	var in bootstrapPayload
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Malformed bootstrap request.")
		return
	}
	if s.cfg.BootstrapToken == "" {
		writeError(w, http.StatusForbidden, "bootstrap_closed", "Bootstrap is disabled on this installation.")
		return
	}
	if !security.EqualKey(hashToken(s.cfg.Secret, in.Token), hashToken(s.cfg.Secret, s.cfg.BootstrapToken)) {
		writeError(w, http.StatusForbidden, "bad_token", "The bootstrap token is not valid.")
		return
	}
	count, err := s.st.CountAdmins(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not read the operator table.")
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "already_initialised", "An operator already exists.")
		return
	}
	if msg := validateOperatorInput(in.Username, in.Password); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_operator", msg)
		return
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not hash the password.")
		return
	}
	user, err := s.st.CreateAdmin(r.Context(), strings.TrimSpace(in.Username), hash)
	if err != nil {
		storeError(w, err, "Operator not found.", "That username is taken.")
		return
	}
	if err := s.issueSession(w, r, user); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not start a session.")
		return
	}
	_ = s.st.Audit(r.Context(), user.Username, "operator.bootstrap", user.ID)
	writeJSON(w, http.StatusCreated, user)
}

type loginPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleLogin verifies credentials. The failure message is identical for an
// unknown user and a wrong password so the endpoint cannot enumerate accounts.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in loginPayload
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Malformed sign-in request.")
		return
	}
	user, hash, err := s.st.Credentials(r.Context(), strings.TrimSpace(in.Username))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Spend comparable time so timing does not reveal account existence.
			_ = security.BcryptCompare("$2a$12$0000000000000000000000000000000000000000000000000000", in.Password)
			writeError(w, http.StatusUnauthorized, "bad_credentials", "Username or password is incorrect.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Sign-in is unavailable right now.")
		return
	}
	if !checkPassword(hash, in.Password) {
		writeError(w, http.StatusUnauthorized, "bad_credentials", "Username or password is incorrect.")
		return
	}
	if err := s.issueSession(w, r, user); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not start a session.")
		return
	}
	_ = s.st.TouchLogin(r.Context(), user.ID)
	_ = s.st.Audit(r.Context(), user.Username, "operator.login", user.ID)
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookie); err == nil && cookie.Value != "" {
		_ = s.st.DeleteSession(r.Context(), hashToken(s.cfg.Secret, cookie.Value))
	}
	clearSessionCookie(w, s.cfg)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to continue.")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

type passwordPayload struct {
	Current string `json:"current_password"`
	Next    string `json:"new_password"`
}

// handleChangePassword rotates the operator credential and invalidates every
// other session, which is what makes the change meaningful after a leak.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	var in passwordPayload
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Malformed password request.")
		return
	}
	_, hash, err := s.st.Credentials(r.Context(), user.Username)
	if err != nil || !checkPassword(hash, in.Current) {
		writeError(w, http.StatusForbidden, "bad_password", "The current password is incorrect.")
		return
	}
	if msg := validateOperatorInput(user.Username, in.Next); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_password", msg)
		return
	}
	next, err := hashPassword(in.Next)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not hash the password.")
		return
	}
	if err := s.st.SetPassword(r.Context(), user.ID, next); err != nil {
		storeError(w, err, "Operator not found.", "Could not update the password.")
		return
	}
	if cookie, err := r.Cookie(SessionCookie); err == nil {
		_ = s.st.DeleteOtherSessions(r.Context(), user.ID, hashToken(s.cfg.Secret, cookie.Value))
	}
	_ = s.st.Audit(r.Context(), user.Username, "operator.password_changed", user.ID)
	w.WriteHeader(http.StatusNoContent)
}

// issueSession mints a session token, stores only its hash, and sets the cookie.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, user models.AdminUser) error {
	token, err := security.RandomToken(32)
	if err != nil {
		return err
	}
	expires := time.Now().Add(s.cfg.SessionTTL)
	if err := s.st.CreateSession(r.Context(), hashToken(s.cfg.Secret, token), user.ID, expires); err != nil {
		return err
	}
	setSessionCookie(w, s.cfg, token, expires)
	return nil
}

// validateOperatorInput returns an empty string when the input is acceptable.
//
// The password floor is 8 characters, matching the NIST SP 800-63B baseline for
// memorised secrets. Length is the dominant factor, but a stricter arbitrary
// number buys little: bcrypt cost 12 already makes offline guessing expensive,
// and the operator can and should choose something longer.
func validateOperatorInput(username, password string) string {
	username = strings.TrimSpace(username)
	switch {
	case len(username) < 3 || len(username) > 64:
		return "Username must be between 3 and 64 characters."
	case strings.ContainsAny(username, " \t\n/@:"):
		return "Username must not contain spaces, slashes or colons."
	case len(password) < 8:
		return "Use a password of at least 8 characters."
	case len(password) > 200:
		return "Password is too long."
	}
	return ""
}
