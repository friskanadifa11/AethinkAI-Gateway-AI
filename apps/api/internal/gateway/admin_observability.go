package gateway

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/store"
)

// allowedRanges maps the Overview range selector to a lookback and a chart
// bucket. Anything unrecognised falls back to 24h/hour rather than erroring.
var allowedRanges = map[string]struct {
	Lookback time.Duration
	Bucket   string
}{
	"1h":  {time.Hour, "minute"},
	"24h": {24 * time.Hour, "hour"},
	"7d":  {7 * 24 * time.Hour, "hour"},
	"30d": {30 * 24 * time.Hour, "day"},
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("range")
	spec, ok := allowedRanges[key]
	if !ok {
		key, spec = "24h", allowedRanges["24h"]
	}
	since := time.Now().Add(-spec.Lookback)
	o, err := s.st.Overview(r.Context(), since, spec.Bucket)
	if err != nil {
		storeError(w, err, "Overview unavailable.", "Overview unavailable.")
		return
	}
	o.Range = key
	// Overview embeds the provider list; redact hosts the same way the provider
	// page does so the two never disagree about what is shown.
	for i := range o.Providers {
		o.Providers[i].BaseURL = redactHost(o.Providers[i].BaseURL)
	}
	writeJSON(w, http.StatusOK, o)
}

// handleListRequests serves one page of the request log plus the total count.
func (s *Server) handleListRequests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := store.RequestFilter{
		Since:  parseSince(q.Get("since")),
		Status: q.Get("status"),
		Alias:  q.Get("alias"),
		KeyID:  strings.TrimSpace(q.Get("key_id")),
		Search: strings.TrimSpace(q.Get("q")),
		Limit:  atoiDefault(q.Get("limit"), 50),
		Offset: atoiDefault(q.Get("offset"), 0),
	}
	if filter.Status != "" && filter.Status != "ok" && filter.Status != "error" && filter.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "bad_filter", "Status must be ok, error or rejected.")
		return
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	rows, total, err := s.st.ListRequests(r.Context(), filter)
	if err != nil {
		storeError(w, err, "Request log unavailable.", "Request log unavailable.")
		return
	}
	aliases, err := s.st.DistinctAliases(r.Context())
	if err != nil {
		aliases = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"requests": rows,
		"total":    total,
		"limit":    filter.Limit,
		"offset":   filter.Offset,
		"aliases":  aliases,
	})
}

func (s *Server) handleTraceDetail(w http.ResponseWriter, r *http.Request) {
	d, err := s.st.TraceDetail(r.Context(), r.PathValue("traceId"))
	if err != nil {
		storeError(w, err, "Trace not found.", "Trace not found.")
		return
	}
	// Propagate the parent trace id onto each attempt so the timeline can be
	// rendered without cross-referencing in the browser.
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	set, err := s.st.LoadSettings(r.Context())
	if err != nil {
		storeError(w, err, "Settings unavailable.", "Settings unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, set)
}

type settingsPayload struct {
	SiteName     *string `json:"site_name"`
	PublicAPIURL *string `json:"public_api_url"`
	Timezone     *string `json:"timezone"`
	TimeoutMS    *int    `json:"default_timeout_ms"`
	RetentionDay *int    `json:"retention_days"`
	Motion       *string `json:"motion"`
}

// handlePutSettings writes only the fields present in the body. Partial updates
// keep a console that saves one card from clearing another.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	var in settingsPayload
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Malformed settings payload.")
		return
	}
	values := map[string]string{}
	if in.SiteName != nil {
		name := strings.TrimSpace(*in.SiteName)
		if len(name) < 2 || len(name) > 60 {
			writeError(w, http.StatusBadRequest, "invalid_site_name", "Site name must be between 2 and 60 characters.")
			return
		}
		values["site_name"] = name
	}
	if in.PublicAPIURL != nil {
		u := strings.TrimSpace(*in.PublicAPIURL)
		if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			writeError(w, http.StatusBadRequest, "invalid_api_url", "Public API URL must start with http:// or https://.")
			return
		}
		values["public_api_url"] = u
	}
	if in.Timezone != nil {
		tz := strings.TrimSpace(*in.Timezone)
		if _, err := time.LoadLocation(tz); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_timezone", "Unknown timezone.")
			return
		}
		values["timezone"] = tz
	}
	if in.TimeoutMS != nil {
		if *in.TimeoutMS < 1000 || *in.TimeoutMS > 600000 {
			writeError(w, http.StatusBadRequest, "invalid_timeout", "Default timeout must be between 1000 and 600000 ms.")
			return
		}
		values["default_timeout_ms"] = strconv.Itoa(*in.TimeoutMS)
	}
	if in.RetentionDay != nil {
		if *in.RetentionDay < 1 || *in.RetentionDay > 3650 {
			writeError(w, http.StatusBadRequest, "invalid_retention", "Retention must be between 1 and 3650 days.")
			return
		}
		values["retention_days"] = strconv.Itoa(*in.RetentionDay)
	}
	if in.Motion != nil {
		switch *in.Motion {
		case "system", "full", "reduced":
			values["motion"] = *in.Motion
		default:
			writeError(w, http.StatusBadRequest, "invalid_motion", "Motion must be system, full or reduced.")
			return
		}
	}
	if len(values) == 0 {
		writeError(w, http.StatusBadRequest, "empty_update", "Nothing to update.")
		return
	}
	if err := s.st.SaveSettings(r.Context(), values); err != nil {
		storeError(w, err, "Settings unavailable.", "Settings unavailable.")
		return
	}
	_ = s.st.Audit(r.Context(), user.Username, "settings.updated", strings.Join(keysOf(values), ","))
	set, err := s.st.LoadSettings(r.Context())
	if err != nil {
		storeError(w, err, "Settings unavailable.", "Settings unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, set)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit := atoiDefault(r.URL.Query().Get("limit"), 50)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.st.RecentAudit(r.Context(), limit)
	if err != nil {
		storeError(w, err, "Audit trail unavailable.", "Audit trail unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": rows})
}

// parseSince accepts a duration suffix (30m, 6h, 12d) or an RFC3339 stamp, and
// defaults to the last 24 hours.
func parseSince(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().Add(-24 * time.Hour)
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return time.Now().Add(-d)
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t
	}
	return time.Now().Add(-24 * time.Hour)
}

func atoiDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
