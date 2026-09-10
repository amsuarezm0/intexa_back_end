package handler

import (
	"testing"

	"github.com/intexa/arca-api/internal/domain"
)

func directory() map[string]domain.ThirdParty {
	head := domain.ThirdParty{CustomerID: "cid-0", Identification: "901037916", BranchOffice: 0, Name: "Fosyga", Type: domain.CustomerTypeOther}
	branch := domain.ThirdParty{CustomerID: "cid-1", Identification: "901037916", BranchOffice: 1, Name: "Fosyga Régimen de Excepción", Type: domain.CustomerTypeOther}
	return map[string]domain.ThirdParty{
		domain.ThirdPartyKey("901037916", 0): head,
		domain.ThirdPartyKey("901037916", 1): branch,
		"901037916":                          head, // bare key points at the head office
	}
}

func TestResolveThirdPartyPrefersExactBranch(t *testing.T) {
	got := resolveThirdParty(directory(), "901037916", 1, "")
	if got == nil || got.CustomerID != "cid-1" {
		t.Fatalf("branch 1 should resolve to its own row, got %+v", got)
	}
	if got.Name != "Fosyga Régimen de Excepción" {
		t.Errorf("name: got %q", got.Name)
	}
}

// Documents synced before the branch office was captured default to 0, so they
// must still find the third party rather than showing nothing.
func TestResolveThirdPartyFallsBackToBareIdentification(t *testing.T) {
	dir := directory()
	delete(dir, domain.ThirdPartyKey("901037916", 7))
	got := resolveThirdParty(dir, "901037916", 7, "")
	if got == nil || got.CustomerID != "cid-0" {
		t.Fatalf("unknown branch should fall back to the head office, got %+v", got)
	}
}

// An unsynced third party still yields its identification, so the UI can show
// the id instead of an empty cell.
func TestResolveThirdPartyKeepsKeyWhenUnknown(t *testing.T) {
	got := resolveThirdParty(directory(), "999999999", 0, "siigo-uuid")
	if got == nil {
		t.Fatal("an unknown identification should still return the key")
	}
	if got.CustomerID != "" || got.Name != "" {
		t.Errorf("unknown third party must not invent a customer: %+v", got)
	}
	if got.Identification != "999999999" || got.SiigoID != "siigo-uuid" {
		t.Errorf("key not carried through: %+v", got)
	}
}

// A manual movement has no counterparty at all.
func TestResolveThirdPartyNilWithoutIdentification(t *testing.T) {
	if got := resolveThirdParty(directory(), "", 0, ""); got != nil {
		t.Errorf("no identification should resolve to nil, got %+v", got)
	}
}

// A directory that failed to load must not break resolution.
func TestResolveThirdPartyToleratesNilDirectory(t *testing.T) {
	got := resolveThirdParty(nil, "901037916", 0, "")
	if got == nil || got.Identification != "901037916" {
		t.Fatalf("nil directory should still echo the key, got %+v", got)
	}
}

func TestAttachToTransactionsResolvesEachRow(t *testing.T) {
	txs := []domain.Transaction{
		{ExternalID: "siigo-rc-1", CounterpartyIdentification: "901037916", CounterpartyBranchOffice: 1},
		{ExternalID: "manual-1"},
	}
	attachToTransactions(directory(), txs)

	if txs[0].ThirdParty == nil || txs[0].ThirdParty.Name != "Fosyga Régimen de Excepción" {
		t.Errorf("RC counterparty not resolved: %+v", txs[0].ThirdParty)
	}
	if txs[1].ThirdParty != nil {
		t.Errorf("manual movement should have no third party, got %+v", txs[1].ThirdParty)
	}
}

// Invoices carry a customer_name column that is empty for synced documents;
// the resolved name backfills it so existing consumers keep working.
func TestAttachToInvoicesBackfillsCustomerName(t *testing.T) {
	invoices := []*domain.Invoice{
		{CustomerIdentification: "901037916", CustomerBranchOffice: 0},
		{CustomerIdentification: "901037916", CustomerBranchOffice: 0, CustomerName: "Nombre manual"},
	}
	attachToInvoices(directory(), invoices)

	if invoices[0].CustomerName != "Fosyga" {
		t.Errorf("empty customer name should be filled from customers, got %q", invoices[0].CustomerName)
	}
	if invoices[1].CustomerName != "Nombre manual" {
		t.Errorf("an existing name must not be overwritten, got %q", invoices[1].CustomerName)
	}
}

// A liquidity alert has to name someone. Synced third parties give a name;
// unsynced ones at least give a NIT; only a manual record falls back to
// whatever the document itself recorded.
func TestAlertPartyPrefersNameThenNIT(t *testing.T) {
	cases := []struct {
		name     string
		tp       *domain.ThirdParty
		fallback string
		want     string
	}{
		{"synced third party", &domain.ThirdParty{Name: "Fosyga", Identification: "901037916"}, "algo", "Fosyga"},
		{"unsynced, key only", &domain.ThirdParty{Identification: "901037916"}, "algo", "NIT 901037916"},
		{"no third party", nil, "Cliente manual", "Cliente manual"},
		{"nothing at all", nil, "", ""},
	}
	for _, c := range cases {
		if got := alertParty(c.tp, c.fallback); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
