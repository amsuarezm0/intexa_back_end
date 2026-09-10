package handler

import (
	"strings"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
)

// resolveThirdParty looks a document's counterparty up in a directory loaded
// once per request.
//
// It prefers the exact (identification, branch office) third party and falls
// back to the bare identification, which covers documents synced before the
// branch office was captured. When nothing matches — a third party not synced,
// or deleted in Siigo — it still returns the key that is known, so the caller
// can show the identification instead of an empty cell.
func resolveThirdParty(dir map[string]domain.ThirdParty, identification string, branchOffice int, siigoID string) *domain.ThirdParty {
	if identification == "" {
		return nil
	}
	if tp, ok := dir[domain.ThirdPartyKey(identification, branchOffice)]; ok {
		return &tp
	}
	if tp, ok := dir[identification]; ok {
		return &tp
	}
	return &domain.ThirdParty{
		Identification: identification,
		BranchOffice:   branchOffice,
		SiigoID:        siigoID,
	}
}

// thirdPartyDirectory loads the directory, tolerating failure: a document list
// is still worth returning without counterparty names, so a lookup problem
// degrades the response rather than failing the request.
func thirdPartyDirectory(store repository.Store) map[string]domain.ThirdParty {
	dir, err := store.GetThirdPartyDirectory()
	if err != nil {
		return nil
	}
	return dir
}

// attachToTransactions resolves the counterparty of each transaction in place.
func attachToTransactions(dir map[string]domain.ThirdParty, txs []domain.Transaction) {
	for i := range txs {
		txs[i].ThirdParty = resolveThirdParty(dir,
			txs[i].CounterpartyIdentification, txs[i].CounterpartyBranchOffice, txs[i].CounterpartySiigoID)
	}
}

// alertParty is the one-line description of an alert's counterparty: its name
// when the third party is synced, its identification when it is not, and
// whatever the document itself recorded (a manual entry) as a last resort. An
// alert saying only "Cobro Pendiente" with a blank line under it is not
// actionable — the point is knowing who to chase.
func alertParty(tp *domain.ThirdParty, fallback string) string {
	if tp != nil {
		if tp.Name != "" {
			return tp.Name
		}
		if tp.Identification != "" {
			return "NIT " + tp.Identification
		}
	}
	return fallback
}

// normalizeCounterparty settles the third party a manual movement was saved
// with. The client picks one and sends its identification and branch office;
// the Siigo id is filled in here from the directory rather than trusted from
// the request, and a blank identification clears the other two so a movement
// cannot keep a dangling branch office or id after its third party is removed.
//
// An identification that matches nothing is still stored: third parties are
// synced from Siigo on a schedule, and refusing a NIT that has not arrived yet
// would block a legitimate movement.
func normalizeCounterparty(store repository.Store, t *domain.Transaction) {
	t.CounterpartyIdentification = strings.TrimSpace(t.CounterpartyIdentification)
	if t.CounterpartyIdentification == "" {
		t.CounterpartyBranchOffice = 0
		t.CounterpartySiigoID = ""
		return
	}
	dir := thirdPartyDirectory(store)
	if tp := resolveThirdParty(dir, t.CounterpartyIdentification, t.CounterpartyBranchOffice, ""); tp != nil {
		t.CounterpartySiigoID = tp.SiigoID
		// Trust the directory's branch office when the pair resolved exactly,
		// so a stale branch from the client does not persist.
		if tp.CustomerID != "" {
			t.CounterpartyBranchOffice = tp.BranchOffice
		}
	}
}

// attachToTransactionPtrs is the pointer-slice form, for the period payload.
func attachToTransactionPtrs(dir map[string]domain.ThirdParty, txs []*domain.Transaction) {
	for _, t := range txs {
		t.ThirdParty = resolveThirdParty(dir,
			t.CounterpartyIdentification, t.CounterpartyBranchOffice, t.CounterpartySiigoID)
	}
}

// attachToInvoices / attachToPurchases do the same for the document tables,
// where the counterparty is the customer and the provider respectively.
func attachToInvoices(dir map[string]domain.ThirdParty, invoices []*domain.Invoice) {
	for _, inv := range invoices {
		inv.ThirdParty = resolveThirdParty(dir,
			inv.CustomerIdentification, inv.CustomerBranchOffice, inv.CustomerSiigoID)
		if inv.CustomerName == "" && inv.ThirdParty != nil {
			inv.CustomerName = inv.ThirdParty.Name
		}
	}
}

func attachToPurchases(dir map[string]domain.ThirdParty, purchases []*domain.Purchase) {
	for _, pur := range purchases {
		pur.ThirdParty = resolveThirdParty(dir,
			pur.ProviderIdentification, pur.ProviderBranchOffice, pur.ProviderSiigoID)
		if pur.ProviderName == "" && pur.ThirdParty != nil {
			pur.ProviderName = pur.ThirdParty.Name
		}
	}
}
