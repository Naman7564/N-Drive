package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/golang-jwt/jwt/v5"

	"fileservice/internal/middleware"
)

func claimsFromContext(r *http.Request) (jwt.RegisteredClaims, bool) {
	claims, ok := r.Context().Value(middleware.ClaimsKey).(jwt.RegisteredClaims)
	return claims, ok
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
