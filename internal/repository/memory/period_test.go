package memory

import (
	"testing"
	"time"

	"github.com/intexa/arca-api/internal/domain"
)

func month(y int, m time.Month) (time.Time, time.Time) {
	from := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	return from, from.AddDate(0, 1, -1)
}

// references collects the invoice references a period returned, so a test can
// say which documents the window let in without depending on the seed.
func references(t *testing.T, s *Store, from, to time.Time) map[string]string {
	t.Helper()
	data, err := s.GetPeriodData(from, to)
	if err != nil {
		t.Fatalf("GetPeriodData: %v", err)
	}
	out := make(map[string]string)
	for _, inv := range data.Invoices {
		out[inv.Reference] = inv.DueDate
	}
	return out
}

// An agreed payment date moves the document out of the period its original due
// date fell in and into the one it is now expected in — the calendar places it
// on the day the money actually arrives.
func TestPeriodFollowsTheAgreedPaymentDate(t *testing.T) {
	s := New()
	inv := &domain.Invoice{
		ID: "inv-agreed", ExternalID: "ext-agreed", Reference: "FV-TEST-01",
		Date: "2026-03-01", DueDate: "2026-03-20", SecondaryDueDate: "2026-05-08",
		Total: 1000, Balance: 1000, Status: domain.StatusPending,
	}
	if _, err := s.UpsertInvoice(inv); err != nil {
		t.Fatalf("UpsertInvoice: %v", err)
	}

	from, to := month(2026, time.March)
	if _, ok := references(t, s, from, to)[inv.Reference]; ok {
		t.Error("the original due date's month should no longer hold the invoice")
	}

	from, to = month(2026, time.May)
	if _, ok := references(t, s, from, to)[inv.Reference]; !ok {
		t.Error("the agreed date's month should hold the invoice")
	}
}

// Without an agreement the document stays on its own due date.
func TestPeriodKeepsTheDueDateWithoutAnAgreement(t *testing.T) {
	s := New()
	inv := &domain.Invoice{
		ID: "inv-plain", ExternalID: "ext-plain", Reference: "FV-TEST-02",
		Date: "2026-03-01", DueDate: "2026-03-20",
		Total: 1000, Balance: 1000, Status: domain.StatusPending,
	}
	if _, err := s.UpsertInvoice(inv); err != nil {
		t.Fatalf("UpsertInvoice: %v", err)
	}
	from, to := month(2026, time.March)
	if _, ok := references(t, s, from, to)[inv.Reference]; !ok {
		t.Error("an invoice with no agreed date belongs to its due date's month")
	}
}

// An agreed date moves the whole balance to one day, so the original
// installment schedule must stop pulling the document into its own months.
func TestPeriodIgnoresInstallmentsOnceAnAgreementExists(t *testing.T) {
	s := New()
	inv := &domain.Invoice{
		ID: "inv-sched", ExternalID: "ext-sched", Reference: "FV-TEST-03",
		Date: "2026-03-01", DueDate: "2026-03-20", SecondaryDueDate: "2026-05-08",
		Installments: []domain.Installment{
			{DueDate: "2026-03-20", Value: 500},
			{DueDate: "2026-04-20", Value: 500},
		},
		Total: 1000, Balance: 1000, Status: domain.StatusPending,
	}
	if _, err := s.UpsertInvoice(inv); err != nil {
		t.Fatalf("UpsertInvoice: %v", err)
	}
	from, to := month(2026, time.April)
	if _, ok := references(t, s, from, to)[inv.Reference]; ok {
		t.Error("a superseded installment should not pull the invoice into April")
	}
	from, to = month(2026, time.May)
	if _, ok := references(t, s, from, to)[inv.Reference]; !ok {
		t.Error("the agreed date's month should hold the invoice")
	}
}
