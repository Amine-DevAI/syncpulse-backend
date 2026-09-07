package ws

import (
	"log"
	"net/http"
	"strings"
)

// CORSMiddleware adds Cross-Origin Resource Sharing headers to responses.
// For local development, it allows any localhost origin dynamically.
// It also handles preflight OPTIONS requests.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
			w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
		} else {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		if r.Method == "OPTIONS" {
			log.Printf("CORS preflight: %s %s origin=%s", r.Method, r.URL.Path, origin)
			w.WriteHeader(http.StatusOK)
			return
		}

		log.Printf("CORS request: %s %s origin=%s headers=%v", r.Method, r.URL.Path, origin, r.Header)
		next.ServeHTTP(w, r)
	})
}

// CORSMiddlewareFunc wraps an http.HandlerFunc with CORS support.
func CORSMiddlewareFunc(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		CORSMiddleware(http.HandlerFunc(next)).ServeHTTP(w, r)
	}
}
