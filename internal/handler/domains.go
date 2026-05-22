package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/intexa/arca-api/internal/middleware"
	"github.com/intexa/arca-api/internal/repository"
)

type DomainsHandler struct {
	store repository.Store
}

func NewDomainsHandler(store repository.Store) *DomainsHandler {
	return &DomainsHandler{store: store}
}

// GET /api/v1/allowed-domains
func (h *DomainsHandler) List(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	domains, err := h.store.GetAllowedDomains()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"domains": domains})
}

// POST /api/v1/allowed-domains   body: {"domain":"example.com"}
func (h *DomainsHandler) Add(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		Domain string `json:"domain"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	if req.Domain == "" || !strings.Contains(req.Domain, ".") {
		jsonError(w, "domain must be a valid domain name (e.g. example.com)", http.StatusBadRequest)
		return
	}
	if err := h.store.AddAllowedDomain(req.Domain); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"domain": req.Domain, "message": "domain added"})
}

// DELETE /api/v1/allowed-domains/{domain}
func (h *DomainsHandler) Remove(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	domain := chi.URLParam(r, "domain")
	if domain == "" {
		jsonError(w, "domain required", http.StatusBadRequest)
		return
	}
	if err := h.store.RemoveAllowedDomain(domain); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"domain": domain, "message": "domain removed"})
}

// isAdmin returns true when the authenticated user has the admin role.
func isAdmin(r *http.Request) bool {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return false
	}
	role, _ := claims["role"].(string)
	return role == "admin"
}
