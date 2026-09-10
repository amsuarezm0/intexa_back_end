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
	// No document table stores the counterparty name, so name each result from
	// the synced third parties. Documents keep whatever name they already had
	// (manual records), and an unsynced identification still shows as an id.
	dir := thirdPartyDirectory(h.store)
	for i := range results {
		if results[i].Counterparty != "" || results[i].CounterpartyID == "" {
			continue
		}
		if tp, ok := dir[results[i].CounterpartyID]; ok {
			results[i].Counterparty = tp.Name
		}
	}
	jsonOK(w, results)
}
