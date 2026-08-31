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

func TestPaymentReceiptTotalUnchanged(t *testing.T) {
	items := []siigopkg.PaymentReceiptItem{
		{Value: 300, Account: siigopkg.VoucherAccount{Movement: "Credit"}},
		{Value: 200, Account: siigopkg.VoucherAccount{Movement: "Debit"}},
	}
	if got := paymentReceiptTotal(999, items); got != 300 {
		t.Errorf("paymentReceiptTotal = %.2f, want 300 (Credit side only)", got)
	}
	// No Credit lines: fall back to the header total.
	if got := paymentReceiptTotal(999, nil); got != 999 {
		t.Errorf("paymentReceiptTotal fallback = %.2f, want 999", got)
	}
}
