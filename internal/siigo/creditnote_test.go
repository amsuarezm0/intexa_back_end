package siigo

import (
	"encoding/json"
	"testing"
)

// Trimmed from a live GET /v1/credit-notes response.
const sampleCreditNote = `{
  "pagination": { "page": 1, "page_size": 100, "total_results": 29 },
  "results": [ {
    "id": "82d78a7a-eaff-4356-957b-d896259733c4",
    "document": { "id": 3276 },
    "number": 75,
    "name": "NC-1-75",
    "date": "2026-09-07",
    "invoice": { "id": "f4730b24-1f19-444a-89af-56ef55c8e28a", "name": "FV-1-731" },
    "reason": 2,
    "total": 20153785.26,
    "stamp": { "status": "Accepted" }
  } ]
}`

func TestCreditNoteDecodesInvoiceLink(t *testing.T) {
	var resp CreditNoteListResponse
	if err := json.Unmarshal([]byte(sampleCreditNote), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Pagination.TotalResults != 29 || len(resp.Results) != 1 {
		t.Fatalf("pagination: %+v, %d results", resp.Pagination, len(resp.Results))
	}
	nc := resp.Results[0]
	if nc.Name != "NC-1-75" || nc.Reason != 2 {
		t.Errorf("header: name=%q reason=%d", nc.Name, nc.Reason)
	}
	if nc.Invoice.ID != "f4730b24-1f19-444a-89af-56ef55c8e28a" {
		t.Errorf("the invoice link is the whole point: got %q", nc.Invoice.ID)
	}
	if nc.Invoice.Name != "FV-1-731" {
		t.Errorf("invoice name: got %q", nc.Invoice.Name)
	}
	if nc.Total != 20153785.26 {
		t.Errorf("total: got %v", nc.Total)
	}
}

// The RP payload carries its value under "payment", not "total" — the "total"
// key does not exist on that endpoint at all.
func TestPaymentReceiptDecodesPaymentValue(t *testing.T) {
	const rp = `{
	  "id": "x", "number": 1, "name": "RP-1-1", "date": "2024-05-02",
	  "supplier": { "id": "s", "identification": "900", "branch_office": 0 },
	  "payment": { "id": 750, "name": "Transferencia Davivienda 4110", "value": 116620.0 },
	  "items": []
	}`
	var pr PaymentReceipt
	if err := json.Unmarshal([]byte(rp), &pr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pr.Payment.Value != 116620.0 {
		t.Errorf("payment.value: got %v, want 116620", pr.Payment.Value)
	}
	if pr.Payment.Name != "Transferencia Davivienda 4110" {
		t.Errorf("payment.name: got %q", pr.Payment.Name)
	}
}
