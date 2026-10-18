package gateway

import (
	"testing"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
)

// Behaviour contracts for the routing rules. Each case states the operational
// rule it protects, so a refactor that keeps the rule still passes.

func view(id string, enabled bool, timeout int) ProviderView {
	return ProviderView{ID: id, Name: "p-" + id, BaseURL: "https://" + id + ".example/v1",
		Enabled: enabled, TimeoutMS: timeout}
}

func route(targets ...models.RouteTarget) models.Route {
	return models.Route{ID: "r1", Alias: "fast-chat", Version: 1, Published: true,
		Strategy: "priority", Targets: targets}
}

func TestResolveTargetsOrdersByPosition(t *testing.T) {
	r := route(
		models.RouteTarget{Position: 2, ProviderID: "c", UpstreamModel: "m", Enabled: true},
		models.RouteTarget{Position: 0, ProviderID: "a", UpstreamModel: "m", Enabled: true},
		models.RouteTarget{Position: 1, ProviderID: "b", UpstreamModel: "m", Enabled: true},
	)
	views := map[string]ProviderView{"a": view("a", true, 0), "b": view("b", true, 0), "c": view("c", true, 0)}

	got := ResolveTargets(r, views)
	if len(got) != 3 {
		t.Fatalf("expected 3 hops, got %d", len(got))
	}
	for i, want := range []string{"a", "b", "c"} {
		if got[i].ProviderID != want {
			t.Errorf("hop %d: expected provider %s, got %s", i, want, got[i].ProviderID)
		}
	}
}

func TestResolveTargetsDropsUnusableHops(t *testing.T) {
	r := route(
		models.RouteTarget{Position: 0, ProviderID: "off", UpstreamModel: "m", Enabled: false},
		models.RouteTarget{Position: 1, ProviderID: "missing", UpstreamModel: "m", Enabled: true},
		models.RouteTarget{Position: 2, ProviderID: "disabled", UpstreamModel: "m", Enabled: true},
		models.RouteTarget{Position: 3, ProviderID: "ok", UpstreamModel: "m", Enabled: true},
	)
	views := map[string]ProviderView{
		"off":      view("off", true, 0),
		"disabled": view("disabled", false, 0),
		"ok":       view("ok", true, 0),
	}

	got := ResolveTargets(r, views)
	if len(got) != 1 || got[0].ProviderID != "ok" {
		t.Fatalf("expected only the usable hop, got %+v", got)
	}
}

func TestResolveTargetsCanBeEmpty(t *testing.T) {
	// An alias whose every hop is gone must resolve to nothing, so the caller
	// reports 503 instead of inventing a provider.
	r := route(models.RouteTarget{Position: 0, ProviderID: "gone", UpstreamModel: "m", Enabled: true})
	if got := ResolveTargets(r, map[string]ProviderView{}); len(got) != 0 {
		t.Fatalf("expected no hops, got %+v", got)
	}
}

func TestResolveTargetsTimeoutPrecedence(t *testing.T) {
	r := route(models.RouteTarget{Position: 0, ProviderID: "a", UpstreamModel: "m",
		Enabled: true, TimeoutMS: 0})
	// No target timeout and no provider timeout falls back to 60s.
	got := ResolveTargets(r, map[string]ProviderView{"a": view("a", true, 0)})
	if got[0].TimeoutMS != 60000 {
		t.Errorf("expected the 60s default, got %d", got[0].TimeoutMS)
	}

	// The target wins over the provider.
	r = route(models.RouteTarget{Position: 0, ProviderID: "a", UpstreamModel: "m",
		Enabled: true, TimeoutMS: 1500})
	got = ResolveTargets(r, map[string]ProviderView{"a": view("a", true, 9000)})
	if got[0].TimeoutMS != 1500 {
		t.Errorf("expected the target timeout to win, got %d", got[0].TimeoutMS)
	}

	// The provider is used when the target is silent.
	r = route(models.RouteTarget{Position: 0, ProviderID: "a", UpstreamModel: "m", Enabled: true})
	got = ResolveTargets(r, map[string]ProviderView{"a": view("a", true, 9000)})
	if got[0].TimeoutMS != 9000 {
		t.Errorf("expected the provider timeout, got %d", got[0].TimeoutMS)
	}
}

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		status  int
		success bool
		code    string
	}{
		{200, true, ""},
		{201, true, ""},
		{401, false, "upstream_auth"},
		{403, false, "upstream_auth"},
		{404, false, "upstream_model_missing"},
		{408, false, "upstream_timeout"},
		{429, false, "upstream_rate_limited"},
		{500, false, "upstream_error"},
		{503, false, "upstream_error"},
		{400, false, "upstream_rejected"},
	}
	for _, c := range cases {
		ok, code := ClassifyStatus(c.status)
		if ok != c.success || code != c.code {
			t.Errorf("status %d: expected (%v,%q), got (%v,%q)", c.status, c.success, c.code, ok, code)
		}
	}
}

func TestRetryableStatus(t *testing.T) {
	// Client errors are terminal: retrying a 400 spends money for the same answer.
	for _, s := range []int{408, 429, 500, 502, 504} {
		if !RetryableStatus(s) {
			t.Errorf("status %d should be retryable", s)
		}
	}
	for _, s := range []int{400, 401, 403, 404, 422} {
		if RetryableStatus(s) {
			t.Errorf("status %d must not be retryable", s)
		}
	}
}
