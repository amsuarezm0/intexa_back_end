package handler

import (
	"testing"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository/memory"
)

func TestEffectiveDueDatePrefersTheAgreedDate(t *testing.T) {
	if got := domain.EffectiveDueDate("2026-09-01", "2026-10-15"); got != "2026-10-15" {
		t.Errorf("agreed date should win: got %q", got)
	}
	if got := domain.EffectiveDueDate("2026-09-01", ""); got != "2026-09-01" {
		t.Errorf("without an agreement the original stands: got %q", got)
	}
}

func TestDueDateShiftDays(t *testing.T) {
	cases := []struct {
		original, secondary string
		want                int
		ok                  bool
	}{
		{"2026-09-01", "2026-09-16", 15, true},  // pushed out
		{"2026-09-16", "2026-09-01", -15, true}, // pulled in
		{"2026-09-01", "2026-09-01", 0, true},
		{"2026-09-01", "", 0, false}, // no agreement, nothing to report
		{"", "2026-09-01", 0, false},
		{"no es fecha", "2026-09-01", 0, false},
	}
	for _, c := range cases {
		got, ok := domain.DueDateShiftDays(c.original, c.secondary)
		if got != c.want || ok != c.ok {
			t.Errorf("(%q,%q): got (%d,%v), want (%d,%v)", c.original, c.secondary, got, ok, c.want, c.ok)
		}
	}
}

// An agreed date replaces the plan rather than shifting it: the whole balance
// lands on that one day, instead of staying spread over dates both sides have
// abandoned.
func TestPendingOnCollapsesTheScheduleWhenAgreed(t *testing.T) {
	schedule := []domain.Installment{
		{DueDate: "2026-09-10", Value: 400},
		{DueDate: "2026-10-10", Value: 600},
	}
	got := pendingOn(1000, 1000, schedule, "2026-09-10", "2026-11-30")
	if len(got) != 1 {
		t.Fatalf("expected a single payment, got %d: %+v", len(got), got)
	}
	if got[0].DueDate != "2026-11-30" || got[0].Value != 1000 {
		t.Errorf("got %+v, want the full balance on the agreed date", got[0])
	}

	// Nothing agreed: the original schedule stands, untouched.
	orig := pendingOn(1000, 1000, schedule, "2026-09-10", "")
	if len(orig) != 2 {
		t.Errorf("without an agreement the schedule should survive, got %+v", orig)
	}

	// Settled documents put nothing on the calendar, agreed date or not.
	if paid := pendingOn(1000, 0, schedule, "2026-09-10", "2026-11-30"); len(paid) != 0 {
		t.Errorf("a paid document should schedule nothing, got %+v", paid)
	}
}

func TestValidDueDate(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"2026-10-15", "2026-10-15", true},
		{"  2026-10-15  ", "2026-10-15", true},
		{"", "", true}, // clearing the agreement
		{"15/10/2026", "", false},
		{"2026-13-45", "", false},
		{"mañana", "", false},
	} {
		got, ok := validDueDate(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: got (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// The whole point of the field is that Siigo never takes it back. A sync
// rewrites everything else on the document; adding this column to an upsert's
// SET list would wipe every agreement, so this pins the behaviour.
func TestAgreedDateSurvivesASiigoSync(t *testing.T) {
	store := memory.New()
	inv := &domain.Invoice{
		ExternalID: "siigo-inv-abc", Source: "Siigo",
		Date: "2026-09-01", DueDate: "2026-09-30", Total: 1000, Balance: 1000,
		Status: domain.StatusPending, Category: "Ventas",
	}
	if _, err := store.UpsertInvoice(inv); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := store.SetInvoiceSecondaryDueDate(inv.ID, "2026-11-30"); err != nil {
		t.Fatalf("set agreed date: %v", err)
	}

	// Siigo sends the document again, as every sync does. It knows nothing
	// about the agreement.
	resync := &domain.Invoice{
		ExternalID: "siigo-inv-abc", Source: "Siigo",
		Date: "2026-09-01", DueDate: "2026-09-30", Total: 1000, Balance: 800,
		Status: domain.StatusPartial, Category: "Ventas",
	}
	if _, err := store.UpsertInvoice(resync); err != nil {
		t.Fatalf("resync: %v", err)
	}

	got, ok, err := store.GetInvoiceByID(inv.ID)
	if err != nil || !ok {
		t.Fatalf("readback: ok=%v err=%v", ok, err)
	}
	if got.SecondaryDueDate != "2026-11-30" {
		t.Errorf("the sync wiped the agreed date: got %q, want 2026-11-30", got.SecondaryDueDate)
	}
	if got.Balance != 800 || got.Status != domain.StatusPartial {
		t.Errorf("the sync should still own everything else: %+v", got)
	}
}

// Clearing hands the document back to its own due date.
func TestClearingTheAgreedDate(t *testing.T) {
	store := memory.New()
	inv := &domain.Invoice{ExternalID: "siigo-inv-x", Source: "Siigo", Date: "2026-09-01", DueDate: "2026-09-30", Total: 10, Balance: 10}
	store.UpsertInvoice(inv) //nolint
	store.SetInvoiceSecondaryDueDate(inv.ID, "2026-12-01") //nolint
	store.SetInvoiceSecondaryDueDate(inv.ID, "")           //nolint

	got, _, _ := store.GetInvoiceByID(inv.ID)
	invoiceDueDates(got)
	if got.SecondaryDueDate != "" {
		t.Errorf("agreed date not cleared: %q", got.SecondaryDueDate)
	}
	if got.EffectiveDueDate != "2026-09-30" {
		t.Errorf("should fall back to the document's own due date, got %q", got.EffectiveDueDate)
	}
	if got.DueDateShiftDays != nil {
		t.Errorf("no agreement means no shift to report, got %v", *got.DueDateShiftDays)
	}
}

// A manual movement without a due date falls back to its movement date, which
// is what projections and the bell used before manual movements had one.
func TestTransactionDueDatesFallBackToMovementDate(t *testing.T) {
	tx := &domain.Transaction{Date: "2026-09-01"}
	transactionDueDates(tx)
	if tx.EffectiveDueDate != "2026-09-01" {
		t.Errorf("got %q", tx.EffectiveDueDate)
	}

	withDue := &domain.Transaction{Date: "2026-09-01", DueDate: "2026-09-20", SecondaryDueDate: "2026-10-05"}
	transactionDueDates(withDue)
	if withDue.EffectiveDueDate != "2026-10-05" {
		t.Errorf("agreed date should govern, got %q", withDue.EffectiveDueDate)
	}
	if withDue.DueDateShiftDays == nil || *withDue.DueDateShiftDays != 15 {
		t.Errorf("shift should be measured from the due date, not the movement date: %v", withDue.DueDateShiftDays)
	}
}
