package handler

import (
	"net/http"
	"strings"

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

// POST /api/v1/categories   body: {"name":"Marketing","type":"expense"}
// Restricted to roles that can write data (every role except CONSULTA) by the
// route's RequireRole middleware.
func (h *SettingsHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		jsonError(w, "el nombre de la categoría es obligatorio", http.StatusBadRequest)
		return
	}
	// A new category is either income or expense — "both" exists only for the
	// original seed rows and is not offered when creating one.
	catType := domain.CategoryType(strings.ToLower(strings.TrimSpace(req.Type)))
	if catType != domain.CategoryIncome && catType != domain.CategoryExpense {
		jsonError(w, "el tipo debe ser 'income' o 'expense'", http.StatusBadRequest)
		return
	}

	// Names are the foreign key used by transactions and budget lines, so a
	// duplicate is rejected before it reaches the UNIQUE constraint.
	existing, err := h.store.GetCategories()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	for _, c := range existing {
		if strings.EqualFold(c.Name, name) {
			jsonError(w, "ya existe una categoría con ese nombre", http.StatusConflict)
			return
		}
	}

	cat := domain.Category{Name: name, Type: catType}
	if err := h.store.CreateCategory(&cat); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	actor, initial := actorFrom(r)
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: actor, Initial: initial, Action: "Creó categoría",
		Module: "Configuración", Color: "bg-green-500",
	})
	jsonCreated(w, cat)
}

func (h *SettingsHandler) GetActivityLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := h.store.GetActivityLogs()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, logs)
}
