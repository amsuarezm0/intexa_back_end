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
	var s domain.Settings
	if err := decode(r, &s); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// Settings (currency, auto exchange, theme) are personal preferences and are
	// not recorded in the activity log.
	if err := h.store.UpdateSettings(userIDFromRequest(r), s); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
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
