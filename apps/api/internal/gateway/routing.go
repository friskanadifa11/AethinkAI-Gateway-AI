package gateway

import (
	"sort"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/store"
)

// ProviderView is the routing-time view of an upstream: only the fields a
// dispatch decision needs, so the hot path never loads a provider's model list.
type ProviderView struct {
	ID         string
	Name       string
	BaseURL    string
	Adapter    string
	Enabled    bool
	TimeoutMS  int
	Credential string
}

// ViewsFromRefs adapts the dispatch query to the routing view.
func ViewsFromRefs(refs map[string]store.ProviderRef) map[string]ProviderView {
	out := make(map[string]ProviderView, len(refs))
	for id, p := range refs {
		out[id] = ProviderView{
			ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, Adapter: p.Adapter,
			Enabled: p.Enabled, TimeoutMS: p.TimeoutMS, Credential: p.CredentialEnc,
		}
	}
	return out
}

// ViewsFromModels adapts the console's provider list to the routing view. The
// credential is absent by construction; dry-run never needs it.
func ViewsFromModels(list []models.Provider) map[string]ProviderView {
	out := make(map[string]ProviderView, len(list))
	for _, p := range list {
		out[p.ID] = ProviderView{
			ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, Adapter: p.Adapter,
			Enabled: p.Enabled, TimeoutMS: p.TimeoutMS,
		}
	}
	return out
}

// ResolvedTarget is one ready-to-dispatch hop.
type ResolvedTarget struct {
	Position      int
	ProviderID    string
	ProviderName  string
	BaseURL       string
	Adapter       string
	UpstreamModel string
	CredentialEnc string
	TimeoutMS     int
	Retries       int
}

// ResolveTargets turns a published route into the ordered attempt list.
//
// Rules, in order:
//   - targets are ordered by position (0 = primary)
//   - a disabled target is dropped
//   - a target whose provider is missing or disabled is dropped
//   - a timeout falls back to the provider's own, then to 60s
//
// The result can legitimately be empty: that means the alias exists but has no
// usable hop, which the caller reports as 503 rather than inventing one.
func ResolveTargets(route models.Route, views map[string]ProviderView) []ResolvedTarget {
	out := make([]ResolvedTarget, 0, len(route.Targets))
	for _, t := range route.Targets {
		if !t.Enabled {
			continue
		}
		p, ok := views[t.ProviderID]
		if !ok || !p.Enabled {
			continue
		}
		timeout := t.TimeoutMS
		if timeout <= 0 {
			timeout = p.TimeoutMS
		}
		if timeout <= 0 {
			timeout = 60000
		}
		out = append(out, ResolvedTarget{
			Position:      t.Position,
			ProviderID:    p.ID,
			ProviderName:  p.Name,
			BaseURL:       p.BaseURL,
			Adapter:       p.Adapter,
			UpstreamModel: t.UpstreamModel,
			CredentialEnc: p.Credential,
			TimeoutMS:     timeout,
			Retries:       t.Retries,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

// ClassifyStatus maps an upstream HTTP status to the short code recorded on the
// attempt row and shown in the trace drawer.
func ClassifyStatus(status int) (isSuccess bool, code string) {
	switch {
	case status >= 200 && status < 300:
		return true, ""
	case status == 401 || status == 403:
		return false, "upstream_auth"
	case status == 404:
		return false, "upstream_model_missing"
	case status == 408:
		return false, "upstream_timeout"
	case status == 429:
		return false, "upstream_rate_limited"
	case status >= 500:
		return false, "upstream_error"
	default:
		return false, "upstream_rejected"
	}
}

// RetryableStatus reports whether another attempt at the same target is worth
// it. Client errors are terminal: retrying a 400 spends money for one answer.
func RetryableStatus(status int) bool {
	return status == 408 || status == 429 || status >= 500
}

// TargetTimeoutSeconds is used by the console's dry-run preview.
func TargetTimeoutSeconds(t ResolvedTarget) int {
	if t.TimeoutMS <= 0 {
		return 60
	}
	return t.TimeoutMS / 1000
}
