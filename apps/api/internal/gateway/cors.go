package gateway

import (
	"net/http"
	"strings"
)

// cors wraps a public handler with the CORS policy from CORS_ORIGINS.
//
// The public API is for servers by default, so the policy is empty and nothing
// is added. An operator who needs a browser client lists the exact origins; "*"
// is allowed but never combined with credentials, because a wildcard plus
// cookies is how a gateway hands one site's session to another.
func (s *Server) cors(next http.HandlerFunc) http.HandlerFunc {
	allowed := parseOrigins(s.cfg.CORSOrigins)
	if len(allowed) == 0 {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next(w, r)
			return
		}
		// Vary always: a cache must not serve one origin's headers to another.
		w.Header().Add("Vary", "Origin")
		if !originAllowed(origin, allowed) {
			next(w, r)
			return
		}
		if allowed["*"] {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Max-Age", "600")
		w.Header().Set("Access-Control-Expose-Headers", "X-Aethink-Trace, X-Aethink-Fallback")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func parseOrigins(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out[strings.TrimRight(trimmed, "/")] = true
	}
	return out
}

func originAllowed(origin string, allowed map[string]bool) bool {
	if allowed["*"] {
		return true
	}
	return allowed[strings.TrimRight(origin, "/")]
}

// handlePreflight answers OPTIONS on the public prefix. The mux matches methods
// literally, so a preflight would otherwise be a 405 before CORS is consulted.
func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Add("Vary", "Origin")
	allowed := parseOrigins(s.cfg.CORSOrigins)
	if !originAllowed(r.Header.Get("Origin"), allowed) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if allowed["*"] {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
}
