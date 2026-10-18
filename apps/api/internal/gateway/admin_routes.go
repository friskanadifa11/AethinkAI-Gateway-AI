package gateway

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
)

// aliasPattern keeps aliases URL- and log-safe: they appear in request paths,
// metrics labels and the OpenAI "model" field.
var aliasPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type routeTargetPayload struct {
	ProviderID    string `json:"provider_id"`
	UpstreamModel string `json:"upstream_model"`
	TimeoutMS     int    `json:"timeout_ms"`
	Retries       int    `json:"retries"`
	Enabled       *bool  `json:"enabled"`
}

type routePayload struct {
	Alias    string               `json:"alias"`
	Strategy string               `json:"strategy"`
	Targets  []routeTargetPayload `json:"targets"`
}

func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	all, err := s.st.ListRoutes(r.Context())
	if err != nil {
		storeError(w, err, "Routes unavailable.", "Routes unavailable.")
		return
	}
	published := []models.Route{}
	drafts := []models.Route{}
	seenAlias := map[string]bool{}
	for _, rt := range all {
		if rt.Published {
			published = append(published, rt)
			seenAlias[rt.Alias] = true
			continue
		}
		// Only the newest draft per alias is worth showing.
		if !seenAlias[rt.Alias] {
			seenAlias[rt.Alias] = true
			drafts = append(drafts, rt)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"published": published, "drafts": drafts})
}

func (s *Server) handleGetRoute(w http.ResponseWriter, r *http.Request) {
	rt, err := s.st.GetRoute(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err, "Route not found.", "Route not found.")
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

func (s *Server) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	var in routePayload
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Malformed route payload.")
		return
	}
	targets, msg := s.normaliseTargets(r, in)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_route", msg)
		return
	}
	strategy := in.Strategy
	if strategy == "" {
		strategy = "priority"
	}
	rt, err := s.st.CreateRouteVersion(r.Context(), strings.TrimSpace(in.Alias), strategy, targets)
	if err != nil {
		storeError(w, err, "Route not found.", "Could not create that route version.")
		return
	}
	_ = s.st.Audit(r.Context(), user.Username, "route.draft_created", rt.Alias)
	writeJSON(w, http.StatusCreated, rt)
}

func (s *Server) handlePublishRoute(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	rt, err := s.st.PublishRoute(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err, "Route not found.", "Could not publish that version.")
		return
	}
	_ = s.st.Audit(r.Context(), user.Username, "route.published", rt.Alias)
	writeJSON(w, http.StatusOK, rt)
}

func (s *Server) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r.Context())
	id := r.PathValue("id")
	if err := s.st.DeleteRouteVersion(r.Context(), id); err != nil {
		storeError(w, err, "Route not found.",
			"The live version cannot be deleted. Publish a replacement first.")
		return
	}
	_ = s.st.Audit(r.Context(), user.Username, "route.version_deleted", id)
	w.WriteHeader(http.StatusNoContent)
}

// handleDryRun reports the exact order the router would try, using only stored
// state. It never contacts a provider, so previewing costs nothing.
func (s *Server) handleDryRun(w http.ResponseWriter, r *http.Request) {
	rt, err := s.st.GetRoute(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err, "Route not found.", "Route not found.")
		return
	}
	providers, err := s.st.ListProviders(r.Context())
	if err != nil {
		storeError(w, err, "Providers unavailable.", "Providers unavailable.")
		return
	}
	byID := ViewsFromModels(providers)
	resolved := ResolveTargets(rt, byID)

	plan := []map[string]any{}
	for i, t := range resolved {
		plan = append(plan, map[string]any{
			"order":          i,
			"provider_id":    t.ProviderID,
			"provider_name":  t.ProviderName,
			"upstream_model": t.UpstreamModel,
			"timeout_s":      TargetTimeoutSeconds(t),
			"retries":        t.Retries,
			"eligible":       true,
		})
	}
	// Explain every target that was dropped, so an empty plan is actionable.
	skipped := []map[string]any{}
	for _, t := range rt.Targets {
		p, ok := byID[t.ProviderID]
		switch {
		case !t.Enabled:
			skipped = append(skipped, map[string]any{"provider_id": t.ProviderID, "reason": "target disabled"})
		case !ok:
			skipped = append(skipped, map[string]any{"provider_id": t.ProviderID, "reason": "provider no longer exists"})
		case !p.Enabled:
			skipped = append(skipped, map[string]any{"provider_id": t.ProviderID, "provider_name": p.Name, "reason": "provider disabled"})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"alias":      rt.Alias,
		"version":    rt.Version,
		"published":  rt.Published,
		"would_use":  plan,
		"skipped":    skipped,
		"would_fail": len(plan) == 0,
	})
}

// normaliseTargets validates the payload and returns the stored shape.
func (s *Server) normaliseTargets(r *http.Request, in routePayload) ([]models.RouteTarget, string) {
	alias := strings.TrimSpace(in.Alias)
	if !aliasPattern.MatchString(alias) {
		return nil, "Alias must start with a letter or digit and contain only a-z, 0-9, dot, dash or underscore."
	}
	if in.Strategy != "" && in.Strategy != "priority" {
		return nil, "Only the priority strategy is available in this release."
	}
	if len(in.Targets) == 0 {
		return nil, "A route needs at least one target."
	}
	if len(in.Targets) > 10 {
		return nil, "A route may have at most 10 targets."
	}
	providers, err := s.st.ListProviders(r.Context())
	if err != nil {
		return nil, "Providers could not be read."
	}
	known := map[string]bool{}
	for _, p := range providers {
		known[p.ID] = true
	}

	out := make([]models.RouteTarget, 0, len(in.Targets))
	seen := map[string]bool{}
	for i, t := range in.Targets {
		if !known[t.ProviderID] {
			return nil, "Target " + itoa(i+1) + " references a provider that does not exist."
		}
		model := strings.TrimSpace(t.UpstreamModel)
		if model == "" {
			return nil, "Target " + itoa(i+1) + " is missing an upstream model."
		}
		key := t.ProviderID + "\x00" + model
		if seen[key] {
			return nil, "Target " + itoa(i+1) + " duplicates an earlier provider and model."
		}
		seen[key] = true
		enabled := true
		if t.Enabled != nil {
			enabled = *t.Enabled
		}
		timeout := t.TimeoutMS
		if timeout <= 0 {
			timeout = 60000
		}
		out = append(out, models.RouteTarget{
			ProviderID:    t.ProviderID,
			UpstreamModel: model,
			TimeoutMS:     timeout,
			Retries:       t.Retries,
			Enabled:       enabled,
		})
	}
	return out, ""
}

func itoa(n int) string { return strconv.Itoa(n) }
