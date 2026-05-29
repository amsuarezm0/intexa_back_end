package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/intexa/arca-api/internal/middleware"
)

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(v)
}

func jsonCreated(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// actorFrom extracts the display name and first-letter initial from the JWT
// claims stored in the request context. Falls back to "Sistema" / "SI" when
// the request has no authenticated user (e.g. scheduled jobs).
func actorFrom(r *http.Request) (name, initial string) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return "Sistema", "SI"
	}
	// Prefer the "name" claim, fall back to email ("sub")
	n, _ := claims["name"].(string)
	if n == "" {
		n, _ = claims["sub"].(string)
	}
	if n == "" {
		return "Sistema", "SI"
	}
	r0, _ := utf8.DecodeRuneInString(strings.ToUpper(n))
	return n, string(r0)
}
