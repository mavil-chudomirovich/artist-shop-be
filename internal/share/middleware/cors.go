package middleware

import (
	"net/http"

	"github.com/go-chi/cors"
)

// CORS applies a deny-by-default cross-origin policy using the configured
// origin allowlist (FR-012).
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	options := cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", CorrelationHeader},
		ExposedHeaders:   []string{CorrelationHeader},
		AllowCredentials: len(allowedOrigins) > 0,
		MaxAge:           300,
	}
	return cors.Handler(options)
}
