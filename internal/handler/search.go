package handler

import (
	"net/http"
	"strings"

	"github.com/intexa/arca-api/internal/repository"
)

type SearchHandler struct{ store repository.Store }

func NewSearchHandler(store repository.Store) *SearchHandler {
	return &SearchHandler{store: store}
}

func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimSpace(r.URL.Query().Get("reference"))
	if ref == "" {
		jsonOK(w, []struct{}{})
		return
	}
	results, err := h.store.Search(ref)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, results)
}
