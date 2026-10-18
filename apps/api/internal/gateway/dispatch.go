// Package gateway wires the HTTP surface: the OpenAI-compatible public API, the
// operator admin API, and the console.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrUpstreamUnavailable marks a transport-level failure (connect, TLS, timeout)
// as opposed to an upstream that answered with an error status.
var ErrUpstreamUnavailable = errors.New("upstream unavailable")

// Upstream is a thin OpenAI-compatible client. Each provider is one base URL.
type Upstream struct {
	Client *http.Client
}

// NewUpstream builds a client with connection reuse and no global timeout: each
// attempt gets its own deadline from the route target.
func NewUpstream() *Upstream {
	transport := &http.Transport{
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	return &Upstream{Client: &http.Client{
		Transport: transport,
		// Streaming responses must not be cut off by a client-level timeout;
		// per-attempt context deadlines do the bounding instead.
		Timeout: 0,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			// A redirect could forward the provider credential to another host.
			return http.ErrUseLastResponse
		},
	}}
}

// RewriteModel replaces the "model" field in an OpenAI-shaped body. Every other
// field the client sent is preserved verbatim, so parameters this gateway does
// not model still reach the provider.
func RewriteModel(body []byte, upstreamModel string) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("request body is not a JSON object: %w", err)
	}
	encoded, err := json.Marshal(upstreamModel)
	if err != nil {
		return nil, err
	}
	payload["model"] = encoded
	return json.Marshal(payload)
}

// Call performs one attempt against a provider.
//
// The caller owns the returned response body and must close it. A non-2xx
// response is returned rather than turned into an error so the caller can
// classify the status and decide whether to fail over.
func (u *Upstream) Call(ctx context.Context, provider BaseProvider, credential string, body []byte, stream bool) (*http.Response, error) {
	endpoint, err := chatEndpoint(provider.GetBaseURL())
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	return resp, nil
}

// Probe performs the bounded connection check behind "Test connection".
// It never triggers paid inference: it lists models instead.
func (u *Upstream) Probe(ctx context.Context, provider BaseProvider, credential string) (int, string, error) {
	endpoint, err := modelsEndpoint(provider.GetBaseURL())
	if err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, "", err
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return resp.StatusCode, strings.TrimSpace(string(snippet)), nil
}

// BaseProvider is the minimum a dispatch or probe needs.
type BaseProvider interface {
	GetBaseURL() string
}

func chatEndpoint(base string) (string, error) {
	return joinPath(base, "/chat/completions")
}

func modelsEndpoint(base string) (string, error) {
	return joinPath(base, "/models")
}

// joinPath appends a suffix to a base URL, tolerating a base that already ends
// in /v1 (the common OpenAI-compatible layout) or does not.
func joinPath(base, suffix string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", fmt.Errorf("provider base URL is not valid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("provider base URL must be http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("provider base URL has no host")
	}
	trimmed := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(trimmed, "/v1") && !strings.HasSuffix(trimmed, "/v1beta") {
		trimmed += "/v1"
	}
	parsed.Path = trimmed + suffix
	return parsed.String(), nil
}
