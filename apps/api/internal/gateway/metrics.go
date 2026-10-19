package gateway

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/security"
)

// Metrics keeps the counters a Prometheus scrape needs, in process.
//
// Per-process is the point, not a shortcut: a scrape carries the instance label,
// so three replicas produce three series that add up, instead of one shared
// number that hides which replica is slow.
type Metrics struct {
	mu           sync.Mutex
	requests     map[[2]string]uint64
	durationSum  map[string]float64
	durationN    map[string]uint64
	fallbacks    map[string]uint64
	limited      map[string]uint64
	breakerOpens map[string]uint64
	started      time.Time
}

// NewMetrics returns an empty registry.
func NewMetrics() *Metrics {
	return &Metrics{
		requests:     map[[2]string]uint64{},
		durationSum:  map[string]float64{},
		durationN:    map[string]uint64{},
		fallbacks:    map[string]uint64{},
		limited:      map[string]uint64{},
		breakerOpens: map[string]uint64{},
		started:      time.Now(),
	}
}

// ObserveRequest records one finished call: its alias, outcome and duration.
func (m *Metrics) ObserveRequest(alias, status string, durationMS, fallbacks int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[[2]string{alias, status}]++
	m.durationSum[alias] += float64(durationMS)
	m.durationN[alias]++
	if fallbacks > 0 {
		m.fallbacks[alias]++
	}
}

// ObserveLimit records a refusal, by reason, so a caller hitting a wall is
// visible as a number rather than only as a support question.
func (m *Metrics) ObserveLimit(reason string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.limited[reason]++
}

// ObserveBreakerOpen records that a provider was taken out of rotation.
func (m *Metrics) ObserveBreakerOpen(provider string) {
	if m == nil || provider == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.breakerOpens[provider]++
}

// handleMetrics answers a Prometheus scrape.
//
// The endpoint is closed unless METRICS_TOKEN is set: this gateway is routinely
// reachable through a public tunnel, and traffic counts with alias names are
// not something to publish by default.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.cfg.MetricsToken == "" {
		http.NotFound(w, r)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if !security.EqualKey(token, s.cfg.MetricsToken) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "A metrics token is required.")
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(s.met.Render()))
}

// Render serialises the registry in the text exposition format. Keys are sorted
// so two scrapes of an unchanged gateway are byte-identical.
func (m *Metrics) Render() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder
	b.WriteString("# HELP aethink_requests_total Chat completion requests by alias and outcome.\n")
	b.WriteString("# TYPE aethink_requests_total counter\n")
	reqKeys := make([][2]string, 0, len(m.requests))
	for k := range m.requests {
		reqKeys = append(reqKeys, k)
	}
	sort.Slice(reqKeys, func(i, j int) bool {
		if reqKeys[i][0] != reqKeys[j][0] {
			return reqKeys[i][0] < reqKeys[j][0]
		}
		return reqKeys[i][1] < reqKeys[j][1]
	})
	for _, k := range reqKeys {
		fmt.Fprintf(&b, "aethink_requests_total{alias=%q,status=%q} %d\n", k[0], k[1], m.requests[k])
	}

	b.WriteString("# HELP aethink_request_duration_milliseconds_sum Total time spent per alias.\n")
	b.WriteString("# TYPE aethink_request_duration_milliseconds_sum counter\n")
	b.WriteString("# HELP aethink_request_duration_milliseconds_count Calls measured per alias.\n")
	b.WriteString("# TYPE aethink_request_duration_milliseconds_count counter\n")
	for _, alias := range sortedKeys(m.durationSum) {
		fmt.Fprintf(&b, "aethink_request_duration_milliseconds_sum{alias=%q} %.0f\n", alias, m.durationSum[alias])
		fmt.Fprintf(&b, "aethink_request_duration_milliseconds_count{alias=%q} %d\n", alias, m.durationN[alias])
	}

	writeSimple(&b, "aethink_fallbacks_total", "Requests that needed a second target, per alias.", m.fallbacks)
	writeSimple(&b, "aethink_limit_refusals_total", "Requests refused by a per-key limit, by reason.", m.limited)
	writeSimple(&b, "aethink_breaker_opens_total", "Times a provider was taken out of rotation.", m.breakerOpens)

	b.WriteString("# HELP aethink_uptime_seconds Seconds since this process started.\n")
	b.WriteString("# TYPE aethink_uptime_seconds gauge\n")
	fmt.Fprintf(&b, "aethink_uptime_seconds %.0f\n", time.Since(m.started).Seconds())
	return b.String()
}

func writeSimple(b *strings.Builder, name, help string, values map[string]uint64) {
	b.WriteString("# HELP " + name + " " + help + "\n")
	b.WriteString("# TYPE " + name + " counter\n")
	for _, label := range sortedKeys(values) {
		key := "reason"
		if name == "aethink_fallbacks_total" {
			key = "alias"
		}
		if name == "aethink_breaker_opens_total" {
			key = "provider"
		}
		fmt.Fprintf(b, "%s{%s=%q} %d\n", name, key, label, values[label])
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
