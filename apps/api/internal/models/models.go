// Package models holds the domain types shared by the store, gateway and API
// layers. Nothing here depends on the database or the HTTP layer.
package models

import "time"

// Provider is a configured upstream. The credential is never returned to a
// caller: only CredentialSet reports whether one is stored.
type Provider struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Adapter       string     `json:"adapter"`
	BaseURL       string     `json:"base_url"`
	Enabled       bool       `json:"enabled"`
	TimeoutMS     int        `json:"timeout_ms"`
	CredentialSet bool       `json:"credential_set"`
	LastTestAt    *time.Time `json:"last_test_at,omitempty"`
	LastTestState string     `json:"last_test_state"`
	LastTestError string     `json:"last_test_error,omitempty"`
	Models        []string   `json:"models"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Route is an alias plus one immutable version. Publishing flips
// Published on the version selected by PublishRoute.
type Route struct {
	ID          string        `json:"id"`
	Alias       string        `json:"alias"`
	Version     int           `json:"version"`
	Published   bool          `json:"published"`
	Strategy    string        `json:"strategy"`
	Targets     []RouteTarget `json:"targets"`
	CreatedAt   time.Time     `json:"created_at"`
	PublishedAt *time.Time    `json:"published_at,omitempty"`
}

// RouteTarget is one ordered hop. Position 0 is the primary; the rest are
// fallbacks consulted in ascending order.
type RouteTarget struct {
	Position      int    `json:"position"`
	ProviderID    string `json:"provider_id"`
	ProviderName  string `json:"provider_name,omitempty"`
	Enabled       bool   `json:"enabled"`
	UpstreamModel string `json:"upstream_model"`
	TimeoutMS     int    `json:"timeout_ms"`
	Retries       int    `json:"retries"`
}

// ClientKey is an API key record. The plaintext is shown once at creation and
// is not recoverable afterwards: only KeyHash is stored, and the identifier
// returned to the console is the non-secret prefix.
type ClientKey struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	OwnerLabel string     `json:"owner_label"`
	Prefix     string     `json:"prefix"`
	Enabled    bool       `json:"enabled"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RPM        int        `json:"rpm"`
	MaxConc    int        `json:"max_concurrency"`
	TokenQuota int64      `json:"token_quota"`
	TokensUsed int64      `json:"tokens_used"`
	Allowlist  []string   `json:"allowed_aliases"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// RequestLog is one accepted gateway request.
type RequestLog struct {
	TraceID      string    `json:"trace_id"`
	KeyID        *string   `json:"key_id,omitempty"`
	KeyLabel     string    `json:"key_label,omitempty"`
	Alias        string    `json:"alias"`
	ProviderName string    `json:"provider_name,omitempty"`
	UpstreamMod  string    `json:"upstream_model,omitempty"`
	Status       string    `json:"status"`
	HTTPStatus   int       `json:"http_status"`
	DurationMS   int       `json:"duration_ms"`
	TTFTMS       *int      `json:"ttft_ms,omitempty"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Fallbacks    int       `json:"fallback_count"`
	ErrorCode    string    `json:"error_code,omitempty"`
	Streamed     bool      `json:"streamed"`
	CreatedAt    time.Time `json:"created_at"`
}

// Attempt is one upstream hop belonging to a RequestLog.
type Attempt struct {
	AttemptNo     int       `json:"attempt_no"`
	ProviderName  string    `json:"provider_name"`
	UpstreamModel string    `json:"upstream_model"`
	Status        string    `json:"status"`
	HTTPStatus    int       `json:"http_status"`
	DurationMS    int       `json:"duration_ms"`
	ErrorCode     string    `json:"error_code,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// RequestDetail is a request plus its ordered attempts.
type RequestDetail struct {
	RequestLog
	Attempts []Attempt `json:"attempts"`
}

// Overview is the Overview page payload. Every field is derived from stored
// request rows; an installation with no traffic returns zeroes, never sample
// figures.
type Overview struct {
	Range       string       `json:"range"`
	Requests    int          `json:"requests"`
	Success     int          `json:"success"`
	Errors      int          `json:"errors"`
	Fallbacks   int          `json:"fallback_events"`
	P95MS       int          `json:"p95_latency_ms"`
	TokensIn    int64        `json:"tokens_in"`
	TokensOut   int64        `json:"tokens_out"`
	Series      []SeriesPt   `json:"series"`
	Providers   []Provider   `json:"providers"`
	RecentFails []RequestLog `json:"recent_failures"`
	Routes      int          `json:"published_routes"`
	Keys        int          `json:"active_keys"`
}

// SeriesPt is one bucket of the traffic chart.
type SeriesPt struct {
	T      time.Time `json:"t"`
	OK     int       `json:"ok"`
	Failed int       `json:"failed"`
}

// AdminUser is an operator account.
type AdminUser struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	CreatedAt time.Time  `json:"created_at"`
	LastLogin *time.Time `json:"last_login_at,omitempty"`
}

// Settings holds the small set of operator-editable values.
type Settings struct {
	SiteName     string `json:"site_name"`
	PublicAPIURL string `json:"public_api_url"`
	Timezone     string `json:"timezone"`
	TimeoutMS    int    `json:"default_timeout_ms"`
	RetentionDay int    `json:"retention_days"`
	Motion       string `json:"motion"`
}
