package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/security"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/store"
)

// dispatch walks the resolved target list, retrying retryable failures on the
// same target and failing over to the next once a target is exhausted.
//
// The response is only committed to the client after an upstream answers 2xx,
// so a failed primary is fully transparent to the caller.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, plan dispatchPlan) {
	attempts := make([]models.Attempt, 0, len(plan.Targets))
	var (
		lastStatus = http.StatusServiceUnavailable
		lastCode   = "no_healthy_target"
		usedTarget *ResolvedTarget
		fallbacks  int
	)

	for i := range plan.Targets {
		t := plan.Targets[i]
		if r.Context().Err() != nil {
			// The caller is gone; stop spending provider tokens.
			break
		}
		// A provider that just failed three times running is skipped rather than
		// costing this caller its full timeout before the same failover.
		if !s.brk.Available(r.Context(), t.ProviderID) {
			attempts = append(attempts, attemptRow(t, "skipped", 0, 0, "circuit_open"))
			lastStatus, lastCode = http.StatusServiceUnavailable, "circuit_open"
			continue
		}
		credential, err := s.credentialFor(r.Context(), t)
		if err != nil {
			attempts = append(attempts, attemptRow(t, "error", 0, 0, "credential_unavailable"))
			lastStatus, lastCode = http.StatusServiceUnavailable, "credential_unavailable"
			fallbacks++
			continue
		}

		tries := t.Retries + 1
		if tries < 1 {
			tries = 1
		}
		targetFailed := false
		for attempt := 0; attempt < tries; attempt++ {
			started := time.Now()
			upstreamBody, err := prepareBody(plan.Body, t.UpstreamModel, plan.Stream)
			if err != nil {
				openAIError(w, http.StatusBadRequest, "invalid_json", "The request body must be a JSON object.", "invalid_request_error")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), time.Duration(t.TimeoutMS)*time.Millisecond)
			resp, err := s.up.Call(ctx, adapterURL{t.BaseURL}, credential, upstreamBody, plan.Stream)
			if err != nil {
				cancel()
				dur := millis(started)
				if errors.Is(err, context.Canceled) {
					return
				}
				code := "upstream_unavailable"
				if errors.Is(err, context.DeadlineExceeded) {
					code = "upstream_timeout"
				}
				attempts = append(attempts, attemptRow(t, "error", 0, dur, code))
				lastStatus, lastCode = http.StatusServiceUnavailable, code
				targetFailed = true
				// A timeout is worth retrying; a refused connection usually is
				// not, but one more try costs less than failing the request.
				continue
			}

			if ok, _ := ClassifyStatus(resp.StatusCode); ok {
				usedTarget = &t
				s.noteHealth(true, t.ProviderID)
				if plan.Stream {
					ttft, tokensIn, tokensOut, streamErr := s.streamThrough(w, resp)
					cancel()
					attempts = append(attempts, attemptRow(t, "ok", resp.StatusCode, millis(started), ""))
					status := "ok"
					errCode := ""
					if streamErr != nil {
						// The head was already sent, so the client sees a
						// truncated stream. Record it as an error, honestly.
						status, errCode = "error", "stream_interrupted"
					}
					s.finish(plan, attempts, status, 200, fallbacks, tokensIn, tokensOut, ttft, errCode, true, *usedTarget)
					if streamErr != nil {
						return
					}
					return
				}
				tokensIn, tokensOut, buffered, readErr := bufferResponse(resp)
				cancel()
				if readErr != nil {
					attempts = append(attempts, attemptRow(t, "error", resp.StatusCode, millis(started), "upstream_read_failed"))
					lastStatus, lastCode = http.StatusBadGateway, "upstream_read_failed"
					targetFailed = true
					continue
				}
				attempts = append(attempts, attemptRow(t, "ok", resp.StatusCode, millis(started), ""))
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("X-Aethink-Trace", plan.TraceID)
				if fallbacks > 0 {
					w.Header().Set("X-Aethink-Fallback", "true")
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(buffered)
				s.finish(plan, attempts, "ok", http.StatusOK, fallbacks, tokensIn, tokensOut, nil, "", false, t)
				return
			}

			// Upstream refused: read a bounded snippet for logging, then close.
			dur := millis(started)
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			cancel()
			_, code := ClassifyStatus(resp.StatusCode)
			attempts = append(attempts, attemptRow(t, "error", resp.StatusCode, dur, code))
			lastStatus, lastCode = resp.StatusCode, code
			targetFailed = true
			if !RetryableStatus(resp.StatusCode) {
				// A 400/404 will not improve on a second try.
				break
			}
		}
		if targetFailed {
			fallbacks++
			s.noteHealth(false, t.ProviderID)
		}
	}

	// Nothing answered. Report the most recent upstream signal, not a generic
	// failure, so the caller can act on it.
	status := http.StatusServiceUnavailable
	switch {
	case lastStatus == http.StatusTooManyRequests:
		status = http.StatusTooManyRequests
	case lastStatus == http.StatusRequestTimeout:
		status = http.StatusGatewayTimeout
	case lastStatus >= 500:
		status = http.StatusBadGateway
	case lastStatus == http.StatusUnauthorized || lastStatus == http.StatusForbidden:
		status = http.StatusBadGateway
	}
	message := "Every provider target for " + plan.Alias + " failed."
	if lastCode == "circuit_open" {
		message = "Every provider target for " + plan.Alias + " is cooling down after recent failures."
	} else if lastCode != "" && lastCode != "no_healthy_target" {
		message = "Every provider target for " + plan.Alias + " failed (" + lastCode + ")."
	}
	s.finish(plan, attempts, "error", status, fallbacks, 0, 0, nil, lastCode, plan.Stream, ResolvedTarget{})
	openAIError(w, status, lastCode, message, "api_error")
}

// noteHealth records an attempt outcome off the request path: a caller that hung
// up still leaves the provider's state accurate for the next one.
func (s *Server) noteHealth(ok bool, providerID string) {
	if s.brk == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if ok {
			s.brk.Succeed(ctx, providerID)
			return
		}
		s.brk.Fail(ctx, providerID)
	}()
}

// credentialFor decrypts a target's provider credential.
func (s *Server) credentialFor(ctx context.Context, t ResolvedTarget) (string, error) {
	if t.CredentialEnc == "" {
		// A local upstream may legitimately need no credential.
		return "", nil
	}
	return security.Decrypt(s.cfg.Secret, t.CredentialEnc)
}

// prepareBody rewrites the model and, for streams, asks for a usage report on
// the final chunk so metering does not depend on counting tokens locally.
func prepareBody(raw []byte, upstreamModel string, stream bool) ([]byte, error) {
	rewritten, err := RewriteModel(raw, upstreamModel)
	if err != nil {
		return nil, err
	}
	if !stream {
		return rewritten, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rewritten, &payload); err != nil {
		return nil, err
	}
	if _, present := payload["stream_options"]; !present {
		payload["stream_options"] = json.RawMessage(`{"include_usage":true}`)
	}
	return json.Marshal(payload)
}

// bufferResponse reads a non-streaming answer and extracts its reported usage.
func bufferResponse(resp *http.Response) (tokensIn, tokensOut int, body []byte, err error) {
	defer resp.Body.Close()
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return 0, 0, nil, err
	}
	var parsed struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Usage != nil {
		tokensIn, tokensOut = parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens
	}
	return tokensIn, tokensOut, body, nil
}

// streamThrough pipes an SSE stream to the client, flushing as it goes. It
// returns the time-to-first-token and any usage the provider reported.
func (s *Server) streamThrough(w http.ResponseWriter, resp *http.Response) (ttft *int, tokensIn, tokensOut int, err error) {
	defer resp.Body.Close()
	flusher, canFlush := w.(http.Flusher)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if canFlush {
		flusher.Flush()
	}

	start := time.Now()
	reader := bufio.NewReaderSize(resp.Body, 32*1024)
	wrote := false
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			payload := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(line)), "data:"))
			if strings.HasPrefix(payload, "{") {
				var chunk struct {
					Usage *struct {
						PromptTokens     int `json:"prompt_tokens"`
						CompletionTokens int `json:"completion_tokens"`
					} `json:"usage"`
				}
				if json.Unmarshal([]byte(payload), &chunk) == nil && chunk.Usage != nil {
					tokensIn, tokensOut = chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens
				}
			}
			if _, writeErr := w.Write(line); writeErr != nil {
				return ttft, tokensIn, tokensOut, writeErr
			}
			if !wrote {
				ms := millis(start)
				ttft = &ms
				wrote = true
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return ttft, tokensIn, tokensOut, nil
			}
			return ttft, tokensIn, tokensOut, readErr
		}
	}
}

// finish writes the trace row on a detached context so a cancelled client still
// leaves an auditable record.
func (s *Server) finish(plan dispatchPlan, attempts []models.Attempt, status string, httpStatus, fallbacks, tokensIn, tokensOut int, ttft *int, errCode string, streamed bool, used ResolvedTarget) {
	keyID := plan.Key.ID
	providerName, upstreamModel := "", ""
	if used.ProviderID != "" {
		providerName, upstreamModel = used.ProviderName, used.UpstreamModel
	}
	duration := millis(plan.Start)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.st.InsertTrace(ctx, store.NewTrace{
			TraceID:       plan.TraceID,
			KeyID:         &keyID,
			KeyLabel:      plan.Key.Label,
			Alias:         plan.Alias,
			ProviderName:  providerName,
			UpstreamModel: upstreamModel,
			Status:        status,
			HTTPStatus:    httpStatus,
			DurationMS:    duration,
			TTFTMS:        ttft,
			InputTokens:   tokensIn,
			OutputTokens:  tokensOut,
			Fallbacks:     fallbacks,
			ErrorCode:     errCode,
			Streamed:      streamed,
			Attempts:      attempts,
		})
		if tokensIn+tokensOut > 0 {
			_ = s.st.RecordKeyUsage(ctx, keyID, int64(tokensIn+tokensOut))
		}
	}()
}

func attemptRow(t ResolvedTarget, status string, httpStatus, durationMS int, code string) models.Attempt {
	return models.Attempt{
		ProviderName:  t.ProviderName,
		UpstreamModel: t.UpstreamModel,
		Status:        status,
		HTTPStatus:    httpStatus,
		DurationMS:    durationMS,
		ErrorCode:     code,
	}
}

func millis(t time.Time) int { return int(time.Since(t).Milliseconds()) }
