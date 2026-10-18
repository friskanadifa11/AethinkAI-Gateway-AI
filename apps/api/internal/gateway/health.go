package gateway

import (
	"net/http"
	"time"
)

// handleHealth is a liveness probe: it answers as long as the process runs.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleReady is a readiness probe. The gateway enforces limits and records
// traces through Postgres and Redis, so it is not ready unless both answer.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 2*time.Second)
	defer cancel()
	postgres, redisOK := s.st.Healthy(ctx)
	ready := postgres && redisOK
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"status":   map[bool]string{true: "ready", false: "degraded"}[ready],
		"postgres": postgres,
		"redis":    redisOK,
	})
}
