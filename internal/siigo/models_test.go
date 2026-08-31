package siigo

import (
	"encoding/json"
	"testing"
)

const sampleRC = `{
  "id": "1b437212-602c-49bf-9217-f5f44a46da02",
  "document": { "id": 3268 },
  "number": 257,
  "name": "RC-1-257",
  "date": "2026-08-27",
  "type": "DebtPayment",
  "cost_center": 1782,
  "customer": {
    "id": "5b8def64-c133-41f7-8e42-89101be6d585",
    "identification": "900003663",
    "branch_office": 0
  },
  "items": [
    { "due": { "prefix": "FV-1", "consecutive": 728, "quote": 1, "date": "2026-08-27" }, "value": 17759200.0 },
    { "due": { "prefix": "FV-1", "consecutive": 730, "quote": 1, "date": "2026-08-31" }, "value": 14604528.77 },
    { "account": { "code": "42950502", "movement": "Credit" }, "description": "Ajuste al peso", "value": 0.23 }
  ],
  "payment": { "id": 8054, "name": "Trasnferencia Davivienda 1037", "value": 32363729.0 },
  "metadata": { "created": "2026-08-27T19:06:13.803" }
}`

func TestVoucherDecodesRealPayload(t *testing.T) {
	var v Voucher
	if err := json.Unmarshal([]byte(sampleRC), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if v.Name != "RC-1-257" || v.Number != 257 || v.Type != "DebtPayment" {
		t.Errorf("header: got name=%q number=%d type=%q", v.Name, v.Number, v.Type)
	}
	if v.Document.ID != 3268 {
		t.Errorf("document.id: got %d, want 3268", v.Document.ID)
	}
	if v.Customer.Identification != "900003663" || v.Customer.ID == "" {
		t.Errorf("customer: got %+v", v.Customer)
	}
	if v.Payment.Value != 32363729.0 || v.Payment.Name != "Trasnferencia Davivienda 1037" {
		t.Errorf("payment: got %+v", v.Payment)
	}

	if len(v.Items) != 3 {
		t.Fatalf("items: got %d, want 3", len(v.Items))
	}
	// Due lines carry no account; the adjustment line carries no due.
	if v.Items[0].Due == nil || v.Items[0].Due.Consecutive != 728 || v.Items[0].Account != nil {
		t.Errorf("item 0: got %+v", v.Items[0])
	}
	if v.Items[2].Account == nil || v.Items[2].Account.Movement != "Credit" || v.Items[2].Due != nil {
		t.Errorf("item 2: got %+v", v.Items[2])
	}
	if v.Items[2].Account.Code != "42950502" {
		t.Errorf("item 2 account code: got %q", v.Items[2].Account.Code)
	}

	// The item values reconcile to payment.value.
	var sum float64
	for _, it := range v.Items {
		sum += it.Value
	}
	if diff := sum - v.Payment.Value; diff > 0.005 || diff < -0.005 {
		t.Errorf("items sum %.2f does not reconcile with payment.value %.2f", sum, v.Payment.Value)
	}
}
