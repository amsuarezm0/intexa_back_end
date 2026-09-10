package siigo

import (
	"encoding/json"
	"testing"
)

// Trimmed from live GET /v1/purchases and /v1/payment-receipts responses.
//
// The counterparty arrives under "supplier". It was previously decoded as
// "provider", which silently matched nothing and left every purchase without a
// supplier — these fixtures exist so that regression cannot come back unnoticed.

const samplePurchase = `{
  "id": "e9fd719e-b589-425b-8f5b-0fbbdbcf952a",
  "document": { "id": 3274 },
  "number": 1336,
  "name": "FC-1-1336",
  "date": "2026-09-04",
  "supplier": {
    "id": "9929f839-7331-4312-8cb2-44ced19d3e14",
    "identification": "900934851",
    "branch_office": 0
  },
  "total": 64900.0,
  "balance": 0.0,
  "items": [ { "description": "FC HIFE-300096 SERVICIO DE RESTAURANTE", "total": 64900.0 } ]
}`

const samplePaymentReceipt = `{
  "id": "80b0e312-e545-49fd-b030-1fcbdc1e045c",
  "number": 2305,
  "name": "RP-1-2305",
  "date": "2026-09-08",
  "type": "Detailed",
  "supplier": {
    "id": "0b0710f3-3072-4078-ae34-0bcf4c3a4e96",
    "identification": "1032409331",
    "branch_office": 0
  },
  "items": [ {
    "account": { "code": "13301505", "movement": "Debit" },
    "description": "VIATICOS CALI 09 SEPT",
    "value": 150000.0
  } ]
}`

func TestPurchaseDecodesSupplier(t *testing.T) {
	var p Purchase
	if err := json.Unmarshal([]byte(samplePurchase), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Name != "FC-1-1336" || p.Number != 1336 {
		t.Errorf("header: got name=%q number=%d", p.Name, p.Number)
	}
	if p.Supplier.Identification != "900934851" {
		t.Errorf("supplier.identification: got %q, want 900934851 — the counterparty key was dropped", p.Supplier.Identification)
	}
	if p.Supplier.ID != "9929f839-7331-4312-8cb2-44ced19d3e14" {
		t.Errorf("supplier.id: got %q", p.Supplier.ID)
	}
	if p.Supplier.BranchOffice != 0 {
		t.Errorf("supplier.branch_office: got %d", p.Supplier.BranchOffice)
	}
}

func TestPaymentReceiptDecodesSupplier(t *testing.T) {
	var pr PaymentReceipt
	if err := json.Unmarshal([]byte(samplePaymentReceipt), &pr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pr.Name != "RP-1-2305" {
		t.Errorf("name: got %q", pr.Name)
	}
	if pr.Supplier.Identification != "1032409331" {
		t.Errorf("supplier.identification: got %q, want 1032409331", pr.Supplier.Identification)
	}
	if pr.Supplier.ID != "0b0710f3-3072-4078-ae34-0bcf4c3a4e96" {
		t.Errorf("supplier.id: got %q", pr.Supplier.ID)
	}
}

// Every document list endpoint returns the same slim third-party shape, so the
// key that links a document to a customer is always identification+branch.
func TestDocumentPartyShapeIsSharedAcrossDocuments(t *testing.T) {
	var inv Invoice
	if err := json.Unmarshal([]byte(`{"customer":{"id":"c1","identification":"901100336","branch_office":2}}`), &inv); err != nil {
		t.Fatalf("invoice: %v", err)
	}
	if inv.Customer.Identification != "901100336" || inv.Customer.BranchOffice != 2 {
		t.Errorf("invoice customer: got %+v", inv.Customer)
	}

	var v Voucher
	if err := json.Unmarshal([]byte(`{"customer":{"id":"c2","identification":"901425784","branch_office":0}}`), &v); err != nil {
		t.Fatalf("voucher: %v", err)
	}
	if v.Customer.Identification != "901425784" {
		t.Errorf("voucher customer: got %+v", v.Customer)
	}
}
