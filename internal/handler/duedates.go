package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
)

// ── computed due-date fields ──────────────────────────────────────────────────
//
// A document keeps both dates: the one it came with and the one that was agreed.
// The agreed date governs when the money is expected, and the shift records how
// far it moved, so neither date hides the other.

func invoiceDueDates(inv *domain.Invoice) {
	inv.EffectiveDueDate = domain.EffectiveDueDate(firstNonEmpty(inv.DueDate, inv.Date), inv.SecondaryDueDate)
	if n, ok := domain.DueDateShiftDays(firstNonEmpty(inv.DueDate, inv.Date), inv.SecondaryDueDate); ok {
		inv.DueDateShiftDays = &n
	}
}

func purchaseDueDates(pur *domain.Purchase) {
	pur.EffectiveDueDate = domain.EffectiveDueDate(firstNonEmpty(pur.DueDate, pur.Date), pur.SecondaryDueDate)
	if n, ok := domain.DueDateShiftDays(firstNonEmpty(pur.DueDate, pur.Date), pur.SecondaryDueDate); ok {
		pur.DueDateShiftDays = &n
	}
}

// transactionDueDates works off the movement date when no due date was given —
// that is what projections and the notification bell treated as the due date
// before manual movements had one of their own.
func transactionDueDates(t *domain.Transaction) {
	base := firstNonEmpty(t.DueDate, t.Date)
	t.EffectiveDueDate = domain.EffectiveDueDate(base, t.SecondaryDueDate)
	if n, ok := domain.DueDateShiftDays(base, t.SecondaryDueDate); ok {
		t.DueDateShiftDays = &n
	}
}

func attachDueDatesToInvoices(invoices []*domain.Invoice) {
	for _, inv := range invoices {
		invoiceDueDates(inv)
	}
}

func attachDueDatesToPurchases(purchases []*domain.Purchase) {
	for _, pur := range purchases {
		purchaseDueDates(pur)
	}
}

func attachDueDatesToTransactions(txs []domain.Transaction) {
	for i := range txs {
		transactionDueDates(&txs[i])
	}
}

func attachDueDatesToTransactionPtrs(txs []*domain.Transaction) {
	for _, t := range txs {
		transactionDueDates(t)
	}
}

// ── the one manual edit allowed on a Siigo document ──────────────────────────

type DocumentsHandler struct {
	store repository.Store
}

func NewDocumentsHandler(store repository.Store) *DocumentsHandler {
	return &DocumentsHandler{store: store}
}

type secondaryDueDateRequest struct {
	SecondaryDueDate string `json:"secondaryDueDate"`
}

// validDueDate accepts a YYYY-MM-DD date, or an empty string to clear it.
func validDueDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", false
	}
	return s, true
}

// SetInvoiceSecondaryDueDate records when a sales invoice will actually be paid.
//
// PUT /api/v1/invoices/{id}/secondary-due-date
//
// This is the only write into a Siigo-sourced document. It takes one field and
// nothing else on purpose: everything else is Siigo's and is rewritten on every
// sync, so accepting a fuller body would invite edits that silently vanish.
func (h *DocumentsHandler) SetInvoiceSecondaryDueDate(w http.ResponseWriter, r *http.Request) {
	h.setSecondaryDueDate(w, r, "factura de venta", h.store.SetInvoiceSecondaryDueDate, func(id string) (any, error) {
		inv, ok, err := h.store.GetInvoiceByID(id)
		if err != nil || !ok {
			return nil, err
		}
		invoiceDueDates(inv)
		return inv, nil
	})
}

// PUT /api/v1/purchases/{id}/secondary-due-date
func (h *DocumentsHandler) SetPurchaseSecondaryDueDate(w http.ResponseWriter, r *http.Request) {
	h.setSecondaryDueDate(w, r, "factura de compra", h.store.SetPurchaseSecondaryDueDate, func(id string) (any, error) {
		pur, ok, err := h.store.GetPurchaseByID(id)
		if err != nil || !ok {
			return nil, err
		}
		purchaseDueDates(pur)
		return pur, nil
	})
}

func (h *DocumentsHandler) setSecondaryDueDate(
	w http.ResponseWriter, r *http.Request, label string,
	save func(id, date string) (bool, error),
	reload func(id string) (any, error),
) {
	id := chi.URLParam(r, "id")
	var req secondaryDueDateRequest
	if err := decode(r, &req); err != nil {
		jsonError(w, "Cuerpo de solicitud inválido", http.StatusBadRequest)
		return
	}
	date, ok := validDueDate(req.SecondaryDueDate)
	if !ok {
		jsonError(w, "La fecha de pago acordada debe tener el formato AAAA-MM-DD", http.StatusBadRequest)
		return
	}

	found, err := save(id, date)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		jsonError(w, label+" no encontrada", http.StatusNotFound)
		return
	}

	actor, initial := actorFrom(r)
	action := "Fijó fecha de pago acordada"
	if date == "" {
		action = "Quitó la fecha de pago acordada"
	}
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: actor, Initial: initial,
		Action: action + " · " + label,
		Module: "Documentos", Color: "bg-yellow-500",
	})

	doc, err := reload(id)
	if err != nil || doc == nil {
		jsonOK(w, map[string]any{"id": id, "secondaryDueDate": date})
		return
	}
	jsonOK(w, doc)
}

// pendingOn spreads a document's unpaid balance over the dates it is expected
// on. Without an agreed date this is the document's own installment schedule.
//
// With one, the schedule collapses: the whole outstanding balance lands on the
// agreed date as a single payment. A renegotiated date replaces the plan rather
// than shifting it — keeping the old instalments would put money on dates both
// sides have already abandoned.
func pendingOn(total, balance float64, schedule []domain.Installment, fallback, secondary string) []domain.Installment {
	if secondary != "" {
		if balance <= 0 {
			return nil
		}
		return []domain.Installment{{DueDate: secondary, Value: balance}}
	}
	return domain.PendingInstallments(total, balance, schedule, fallback)
}
