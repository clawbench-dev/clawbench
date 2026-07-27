package middleware

import "net/http"

// NoCache sets Cache-Control: no-store on responses to prevent browser caching.
// This ensures that refresh buttons always fetch fresh data from the server.
func NoCache(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	}
}
