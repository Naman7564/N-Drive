package middleware

import (
	"net/http"
	"strings"
)

// CORS enables cross-origin API access for a UI hosted on another origin.
// Only origins listed explicitly are allowed; credentials are always sent
// with an exact reflected origin (never "*"), which is what browsers require
// when cookies or authorization headers are in play.
type CORS struct {
	allowed map[string]struct{}
}

// NewCORS builds a CORS policy for the given allowed origins. An empty list
// disables cross-origin support entirely, leaving the default same-origin
// behavior unchanged.
func NewCORS(allowed []string) *CORS {
	origins := make(map[string]struct{}, len(allowed))
	for _, origin := range allowed {
		origin = strings.TrimRight(strings.TrimSpace(origin), "/")
		if origin != "" {
			origins[origin] = struct{}{}
		}
	}
	return &CORS{allowed: origins}
}

// Middleware attaches CORS headers to requests from allowed origins and
// answers preflight OPTIONS requests before they reach the routed handlers.
func (c *CORS) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
		if origin != "" {
			w.Header().Add("Vary", "Origin")
			if _, ok := c.allowed[origin]; ok {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token, X-Request-ID")
				h.Set("Access-Control-Expose-Headers", "X-Request-ID")
				h.Set("Access-Control-Max-Age", "86400")
				if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
