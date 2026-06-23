package handler

import (
	"net/http"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/middleware"
	"github.com/intexa/arca-api/internal/repository"
)

type SettingsHandler struct {
	store repository.Store
}

func NewSettingsHandler(store repository.Store) *SettingsHandler {
	return &SettingsHandler{store: store}
}

func userIDFromRequest(r *http.Request) string {
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		if id, _ := claims["sub"].(string); id != "" {
			return id
		}
	}
	return ""
}

func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	s, err := h.store.GetSettings(userIDFromRequest(r))
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, s)
}

func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromRequest(r)
	var s domain.Settings
	if err := decode(r, &s); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Compare against the current settings so a theme-only change (theme is a
	// personal preference) doesn't generate an activity log entry.
	prev, _ := h.store.GetSettings(userID)

	if err := h.store.UpdateSettings(userID, s); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if s.BaseCurrency != prev.BaseCurrency || s.AutoExchangeRate != prev.AutoExchangeRate {
		actor, initial := actorFrom(r)
		h.store.AddActivityLog(domain.ActivityLog{ //nolint
			UserName: actor, Initial: initial, Action: "Actualizó configuración",
			Module: "Configuración", Color: "bg-slate-500",
		})
	}
	jsonOK(w, s)
}

func (h *SettingsHandler) GetCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.store.GetCategories()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, cats)
}

func (h *SettingsHandler) GetActivityLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := h.store.GetActivityLogs()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, logs)
}
