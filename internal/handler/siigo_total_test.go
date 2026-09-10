package handler

import (
	"testing"

	siigopkg "github.com/intexa/arca-api/internal/siigo"
)

func TestVoucherTotalUsesPaymentValue(t *testing.T) {
	v := siigopkg.Voucher{
		Name:    "RC-1-257",
		Payment: siigopkg.VoucherPayment{Value: 32363729.0},
		Items: []siigopkg.VoucherItem{
			{Due: &siigopkg.VoucherDue{Prefix: "FV-1", Consecutive: 728}, Value: 17759200.0},
			{Due: &siigopkg.VoucherDue{Prefix: "FV-1", Consecutive: 730}, Value: 14604528.77},
			{Account: &siigopkg.VoucherAccount{Code: "42950502", Movement: "Credit"}, Description: "Ajuste al peso", Value: 0.23},
		},
	}
	if got := voucherTotal(v); got != 32363729.0 {
		t.Errorf("voucherTotal = %.2f, want 32363729.00", got)
	}
}

// Without payment.value the item sum stands in — the case that previously
// returned 0 because no item carried account.movement == "Debit".
func TestVoucherTotalFallsBackToItemSum(t *testing.T) {
	v := siigopkg.Voucher{
		Items: []siigopkg.VoucherItem{
			{Due: &siigopkg.VoucherDue{Consecutive: 728}, Value: 100.0},
			{Due: &siigopkg.VoucherDue{Consecutive: 730}, Value: 50.5},
		},
	}
	if got := voucherTotal(v); got != 150.5 {
		t.Errorf("voucherTotal = %.2f, want 150.50", got)
	}
}

// This used to assert the opposite — that the item sum beat the first argument,
// which was read from a "total" key. That key does not exist on the RP endpoint;
// the document value is payment.value, exactly as on a voucher. The old contract
// was written "pending a verified RP payload", and the verified payload changed
// it: payment.value now wins, and the items are only a fallback.
func TestPaymentReceiptTotalPrefersPaymentValue(t *testing.T) {
	items := []siigopkg.PaymentReceiptItem{
		{Value: 300, Account: siigopkg.VoucherAccount{Movement: "Credit"}},
		{Value: 200, Account: siigopkg.VoucherAccount{Movement: "Debit"}},
	}
	if got := paymentReceiptTotal(999, items); got != 999 {
		t.Errorf("paymentReceiptTotal = %.2f, want 999 (payment.value)", got)
	}
	// No payment block: only the Credit side is money out. Summing both sides
	// would count the same peso twice, since these receipts are double-entry.
	if got := paymentReceiptTotal(0, items); got != 300 {
		t.Errorf("paymentReceiptTotal fallback = %.2f, want 300 (Credit only)", got)
	}
	if got := paymentReceiptTotal(0, nil); got != 0 {
		t.Errorf("annulled shell = %.2f, want 0", got)
	}
}
