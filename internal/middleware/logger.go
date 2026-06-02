package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestLogger is a Chi-compatible middleware that emits one structured log
// line per request: method, path, status, latency, user, and request-id.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		user, role := "", ""
		if claims, ok := ClaimsFromContext(r.Context()); ok {
			user, _ = claims["email"].(string)
			role, _ = claims["role"].(string)
		}

		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}

		slog.Log(r.Context(), level, "request",
			"method",     r.Method,
			"path",       r.URL.Path,
			"status",     status,
			"latency_ms", time.Since(start).Milliseconds(),
			"user",       user,
			"role",       role,
			"request_id", middleware.GetReqID(r.Context()),
			"ip",         r.RemoteAddr,
		)
	})
}
