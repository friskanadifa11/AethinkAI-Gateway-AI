package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/store"
)

// maxRequestBody bounds a request before it is buffered. Chat bodies are small;
// anything larger is a mistake or an attack on memory.
const maxRequestBody = 4 << 20

// maxResponseBody bounds a buffered non-streaming response.
const maxResponseBody = 8 << 20

type chatEnvelope struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// handleChatCompletions is the public entry point: authenticate, meter, resolve
// the alias, then walk the target list until one upstream answers.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	traceID := newTraceID()

	key, fail := s.authenticate(r)
	if fail != nil {
		s.recordRejection(traceID, key, fail)
		openAIError(w, fail.Status, fail.Code, fail.Message, "authentication_error")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		openAIError(w, http.StatusBadRequest, "body_unreadable", "The request body could not be read.", "invalid_request_error")
		return
	}
	if len(body) > maxRequestBody {
		openAIError(w, http.StatusRequestEntityTooLarge, "body_too_large", "The request body is too large.", "invalid_request_error")
		return
	}

	var env chatEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		openAIError(w, http.StatusBadRequest, "invalid_json", "The request body must be a JSON object.", "invalid_request_error")
		return
	}
	alias := strings.TrimSpace(env.Model)
	if alias == "" {
		openAIError(w, http.StatusBadRequest, "missing_model", "Set \"model\" to a published alias.", "invalid_request_error")
		return
	}
	if !allowed(key, alias) {
		// A key restricted to other aliases must not learn whether this one
		// exists, so this is reported as an unknown model, not a 403.
		s.recordRejection(traceID, key, &authFailure{Status: http.StatusNotFound, Code: "model_not_allowed", Message: "unknown model"})
		openAIError(w, http.StatusNotFound, "model_not_found", "The model "+alias+" does not exist or is not available to this key.", "invalid_request_error")
		return
	}

	// Rate limit before touching the database resolver: the cheapest refusal
	// happens first.
	ok, _, err := s.lim.AllowRPM(r.Context(), s.st.RDB, key.ID, traceID, key.RPM, time.Now())
	if err != nil {
		openAIError(w, http.StatusServiceUnavailable, "limiter_unavailable",
			"The rate limiter is unavailable, so the request was refused.", "api_error")
		return
	}
	if !ok {
		w.Header().Set("Retry-After", "60")
		s.met.ObserveLimit("rate")
		openAIError(w, http.StatusTooManyRequests, "rate_limited",
			"This key has reached its per-minute request limit.", "rate_limit_error")
		return
	}
	release, ok := s.lim.Acquire(r.Context(), s.st.RDB, key.ID, key.MaxConc)
	if !ok {
		w.Header().Set("Retry-After", "5")
		s.met.ObserveLimit("concurrency")
		openAIError(w, http.StatusTooManyRequests, "concurrency_limited",
			"This key already has the maximum number of requests in flight.", "rate_limit_error")
		return
	}
	defer release()

	ctx := r.Context()
	route, err := s.st.ResolveRoute(ctx, alias)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.recordRejection(traceID, key, &authFailure{Status: http.StatusNotFound, Code: "model_not_found", Message: "no published route"})
			openAIError(w, http.StatusNotFound, "model_not_found", "No published route serves "+alias+".", "invalid_request_error")
			return
		}
		openAIError(w, http.StatusInternalServerError, "resolver_failed", "Routing is unavailable right now.", "api_error")
		return
	}

	ids := make([]string, 0, len(route.Targets))
	for _, t := range route.Targets {
		ids = append(ids, t.ProviderID)
	}
	refs, err := s.st.EnabledProvidersByID(ctx, ids)
	if err != nil {
		openAIError(w, http.StatusInternalServerError, "resolver_failed", "Routing is unavailable right now.", "api_error")
		return
	}
	targets := ResolveTargets(route, ViewsFromRefs(refs))
	if len(targets) == 0 {
		openAIError(w, http.StatusServiceUnavailable, "no_healthy_target",
			"The route "+alias+" has no enabled provider target.", "api_error")
		return
	}

	// Every dispatch below shares this enqueued plan result.
	plan := dispatchPlan{
		TraceID: traceID,
		Key:     key,
		Alias:   alias,
		Route:   route,
		Targets: targets,
		Body:    body,
		Stream:  env.Stream,
		Start:   start,
	}
	s.dispatch(w, r, plan)
}

// dispatchPlan carries everything the failover loop needs.
type dispatchPlan struct {
	TraceID string
	Key     store.AuthKey
	Alias   string
	Route   models.Route
	Targets []ResolvedTarget
	Body    []byte
	Stream  bool
	Start   time.Time
}

// newTraceID returns a short, unique, log-friendly identifier.
func newTraceID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// The timestamp alone is still unique enough to correlate one request.
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// recordRejection stores a refused request so the console shows why traffic was
// turned away. It runs detached: a cancelled client must not erase the evidence.
func (s *Server) recordRejection(traceID string, key store.AuthKey, fail *authFailure) {
	status := "rejected"
	if fail.Status == http.StatusTooManyRequests {
		status = "error"
	}
	var keyID *string
	if key.ID != "" {
		id := key.ID
		keyID = &id
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.st.InsertTrace(ctx, store.NewTrace{
			TraceID:    traceID,
			KeyID:      keyID,
			KeyLabel:   key.Label,
			Status:     status,
			HTTPStatus: fail.Status,
			ErrorCode:  fail.Code,
		})
	}()
}
