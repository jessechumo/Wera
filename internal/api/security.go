package api

import (
	"net/http"
	"strings"
)

// securityHeaders sets defensive headers on every API response. JSON is
// never cached by shared caches (it is per user); handlers that serve
// cacheable files (avatars) override Cache-Control.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		// API responses are data, never documents: nothing in them may run.
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; sandbox")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// crossSite reports a request the browser marked as coming from another
// site (Fetch Metadata). Combined with the Origin check, this refuses
// cross-site state changes even from browsers that omit Origin.
func crossSite(r *http.Request) bool {
	return r.Header.Get("Sec-Fetch-Site") == "cross-site"
}
