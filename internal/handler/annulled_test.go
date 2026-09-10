package handler

import (
	"testing"

	"github.com/intexa/arca-api/internal/domain"
)

// An annulled RC/RP is stripped to an empty shell by Siigo — no items, no
// payment. RC-2-195 is the real example: it lands as a 0.00 movement, and
// without this it reads as "Completado", i.e. money settled that never moved.
func TestReceiptStatusDetectsAnnulment(t *testing.T) {
	cases := []struct {
		name    string
		items   int
		payment float64
		want    domain.TransactionStatus
	}{
		{"annulled shell (RC-2-195)", 0, 0, domain.StatusCancelled},
		{"normal receipt with payment", 3, 850000, domain.StatusCompleted},
		{"items but no payment block", 2, 0, domain.StatusCompleted},
		{"payment but no items", 0, 116620, domain.StatusCompleted},
	}
	for _, c := range cases {
		if got := receiptStatus(c.items, c.payment); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// An annulled invoice has balance 0 — identical to a paid one — so only the
// credit notes against it reveal that it is void.
func TestInvoiceStatusWithCredits(t *testing.T) {
	cases := []struct {
		name                     string
		balance, total, credited float64
		want                     domain.TransactionStatus
	}{
		{"paid, no credit note", 0, 1000, 0, domain.StatusCompleted},
		{"annulled in full", 0, 1000, 1000, domain.StatusCancelled},
		{"annulled by several partial notes", 0, 1000, 600 + 400, domain.StatusCancelled},
		{"credited within a cent", 0, 20153785.26, 20153785.25, domain.StatusCancelled},
		{"partially credited stays pending", 1000, 1000, 300, domain.StatusPending},
		{"partial payment, small credit", 400, 1000, 100, domain.StatusPartial},
		{"no total, no annulment", 0, 0, 0, domain.StatusCompleted},
	}
	for _, c := range cases {
		if got := invoiceStatusWithCredits(c.balance, c.total, c.credited); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// An invoice with no credit note keeps exactly the status it had before.
func TestInvoiceStatusUnchangedWithoutCredits(t *testing.T) {
	for _, c := range []struct {
		balance, total float64
		want           domain.TransactionStatus
	}{
		{0, 500, domain.StatusCompleted},
		{200, 500, domain.StatusPartial},
		{500, 500, domain.StatusPending},
	} {
		if got := invoiceStatusWithCredits(c.balance, c.total, 0); got != invoiceStatus(c.balance, c.total) || got != c.want {
			t.Errorf("balance=%v total=%v: got %q, want %q", c.balance, c.total, got, c.want)
		}
	}
}

