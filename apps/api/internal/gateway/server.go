package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/config"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/store"
)

// SessionCookie is the admin session cookie name.
const SessionCookie = "aethink_session"

// Server holds the wired dependencies for every handler.
type Server struct {
	cfg *config.Config
	st  *store.Store
	up  *Upstream
	lim *Limiter
	brk *Breaker
	log *slog.Logger
	mux *http.ServeMux
}

// New builds the server and registers every route.
func New(cfg *config.Config, st *store.Store, log *slog.Logger) *Server {
	s := &Server{
		cfg: cfg,
		st:  st,
		up:  NewUpstream(),
		lim: NewLimiter(),
		brk: NewBreaker(st.RDB),
		log: log,
		mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// --- health -------------------------------------------------------------
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReady)

	// --- public, OpenAI-compatible -----------------------------------------
	s.mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	s.mux.HandleFunc("GET /v1/models", s.handleModels)

	// --- admin, unauthenticated --------------------------------------------
	s.mux.HandleFunc("GET /api/admin/setup-state", s.handleSetupState)
	s.mux.HandleFunc("POST /api/admin/bootstrap", s.handleBootstrap)
	s.mux.HandleFunc("POST /api/admin/login", s.handleLogin)

	// --- admin, session required -------------------------------------------
	s.mux.Handle("POST /api/admin/logout", s.admin(s.handleLogout))
	s.mux.Handle("GET /api/admin/me", s.admin(s.handleMe))
	s.mux.Handle("POST /api/admin/password", s.admin(s.handleChangePassword))
	s.mux.Handle("GET /api/admin/overview", s.admin(s.handleOverview))
	s.mux.Handle("GET /api/admin/providers", s.admin(s.handleListProviders))
	s.mux.Handle("POST /api/admin/providers", s.admin(s.handleCreateProvider))
	s.mux.Handle("PATCH /api/admin/providers/{id}", s.admin(s.handleUpdateProvider))
	s.mux.Handle("DELETE /api/admin/providers/{id}", s.admin(s.handleDeleteProvider))
	s.mux.Handle("POST /api/admin/providers/{id}/test", s.admin(s.handleTestProvider))
	s.mux.Handle("GET /api/admin/routes", s.admin(s.handleListRoutes))
	s.mux.Handle("POST /api/admin/routes", s.admin(s.handleCreateRoute))
	s.mux.Handle("GET /api/admin/routes/{id}", s.admin(s.handleGetRoute))
	s.mux.Handle("POST /api/admin/routes/{id}/publish", s.admin(s.handlePublishRoute))
	s.mux.Handle("POST /api/admin/routes/{id}/dry-run", s.admin(s.handleDryRun))
	s.mux.Handle("DELETE /api/admin/routes/{id}", s.admin(s.handleDeleteRoute))
	s.mux.Handle("GET /api/admin/keys", s.admin(s.handleListKeys))
	s.mux.Handle("POST /api/admin/keys", s.admin(s.handleCreateKey))
	s.mux.Handle("GET /api/admin/keys/{id}", s.admin(s.handleGetKey))
	s.mux.Handle("PATCH /api/admin/keys/{id}", s.admin(s.handleUpdateKey))
	s.mux.Handle("POST /api/admin/keys/{id}/revoke", s.admin(s.handleRevokeKey))
	s.mux.Handle("POST /api/admin/keys/{id}/rotate", s.admin(s.handleRotateKey))
	s.mux.Handle("GET /api/admin/requests", s.admin(s.handleListRequests))
	s.mux.Handle("GET /api/admin/requests/{traceId}", s.admin(s.handleTraceDetail))
	s.mux.Handle("GET /api/admin/settings", s.admin(s.handleGetSettings))
	s.mux.Handle("PUT /api/admin/settings", s.admin(s.handlePutSettings))
	s.mux.Handle("GET /api/admin/audit", s.admin(s.handleAudit))

	// --- console ------------------------------------------------------------
	s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})
	s.mux.Handle("GET /app/", s.console())
}

// Handler returns the outermost middleware chain.
func (s *Server) Handler() http.Handler {
	return s.securityHeaders(s.logRequests(s.mux))
}

// ---- middleware -------------------------------------------------------------

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/app") {
			// The console is same-origin and pulls nothing from a third party.
			h.Set("Content-Security-Policy",
				"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
					"img-src 'self' data:; connect-src 'self'; font-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Flush keeps streaming working through the recorder.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		// Health probes would otherwise dominate the log.
		if r.URL.Path != "/healthz" && r.URL.Path != "/readyz" {
			s.log.Info("http",
				"method", r.Method, "path", r.URL.Path, "status", rec.status,
				"ms", time.Since(start).Milliseconds())
		}
	})
}

// ---- session auth -----------------------------------------------------------

type ctxKey string

const userKey ctxKey = "aethink.user"

// admin wraps a handler so it only runs for a live session.
func (s *Server) admin(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookie)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to continue.")
			return
		}
		hash := hashToken(s.cfg.Secret, cookie.Value)
		user, err := s.st.SessionUser(r.Context(), hash)
		if err != nil {
			// Expired and unknown sessions are indistinguishable to the client.
			clearSessionCookie(w, s.cfg)
			writeError(w, http.StatusUnauthorized, "session_expired", "Your session ended. Sign in again.")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func currentUser(ctx context.Context) (models.AdminUser, bool) {
	u, ok := ctx.Value(userKey).(models.AdminUser)
	return u, ok
}

// ---- console static assets --------------------------------------------------

// console serves the built SPA. Unknown paths inside /app fall back to
// index.html so client-side routes survive a hard refresh.
func (s *Server) console() http.Handler {
	root := s.cfg.WebDist
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if root == "" {
			writeError(w, http.StatusServiceUnavailable, "console_unbuilt",
				"The console bundle is not present in this image (WEB_DIST unset).")
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, "/app/")
		if rel == "" {
			rel = "index.html"
		}
		// filepath.Join cleans the path, so ../ cannot escape the root.
		target := filepath.Join(root, rel)
		if !strings.HasPrefix(target, filepath.Clean(root)) {
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			http.ServeFile(w, r, target)
			return
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})
}

// ---- small response helpers -------------------------------------------------

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var e apiError
	e.Error.Code = code
	e.Error.Message = message
	writeJSON(w, status, e)
}

// decodeJSON rejects unknown fields so a typo in the console surfaces as a 400
// rather than a silently ignored field.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func storeError(w http.ResponseWriter, err error, notFoundMsg, conflictMsg string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", notFoundMsg)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", conflictMsg)
	default:
		writeError(w, http.StatusInternalServerError, "internal", "The gateway could not complete that operation.")
	}
}
