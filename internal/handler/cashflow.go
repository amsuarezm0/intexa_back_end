package handler

import (
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
)

type CashFlowHandler struct {
	store repository.Store
}

func NewCashFlowHandler(store repository.Store) *CashFlowHandler {
	return &CashFlowHandler{store: store}
}

var weekdayES = [7]string{"DOM", "LUN", "MAR", "MIÉ", "JUE", "VIE", "SÁB"}

func (h *CashFlowHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	all, err := h.store.GetAllTransactions()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	invoices, err := h.store.GetAllInvoices()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	purchases, err := h.store.GetAllPurchases()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	now := time.Now()

	// ── Last 7 days (cash received/paid only) ────────────────────────────────
	days := make([]domain.CashFlowDay, 7)
	for i := 6; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		dy, dm, dd := day.Date()
		var ing, egr float64
		for _, t := range all {
			if t.IsProjection {
				continue
			}
			rec := receivedAmount(t)
			if rec == 0 {
				continue
			}
			ty, tm, td := txDate(t)
			if ty == dy && tm == dm && td == dd {
				if t.Type == domain.TypeIngreso {
					ing += rec
				} else {
					egr += rec
				}
			}
		}
		days[6-i] = domain.CashFlowDay{
			Label:    weekdayES[day.Weekday()],
			Date:     dd,
			Ingresos: ing,
			Egresos:  egr,
		}
	}

	// ── Projected balance = real cash ± pending invoices/purchases within 30 days ──
	balance := currentBalance(all)
	horizon := now.AddDate(0, 0, 30)
	var pendingInc, pendingExp float64

	parseDate := func(s string) (time.Time, bool) {
		t, err := time.Parse("2006-01-02", s)
		return t, err == nil
	}

	for _, inv := range invoices {
		if inv.Status == domain.StatusCompleted || inv.Status == domain.StatusCancelled {
			continue
		}
		dateStr := inv.DueDate
		if dateStr == "" {
			dateStr = inv.Date
		}
		if d, ok := parseDate(dateStr); ok && !d.After(horizon) {
			pendingInc += inv.Balance
		}
	}
	for _, pur := range purchases {
		if pur.Status == domain.StatusCompleted || pur.Status == domain.StatusCancelled {
			continue
		}
		dateStr := pur.DueDate
		if dateStr == "" {
			dateStr = pur.Date
		}
		if d, ok := parseDate(dateStr); ok && !d.After(horizon) {
			pendingExp += pur.Balance
		}
	}
	// Manual projections in transactions
	for _, t := range all {
		if !t.IsProjection || t.Status == domain.StatusCancelled {
			continue
		}
		if d, ok := parseDate(t.Date); ok && !d.After(horizon) {
			if t.Type == domain.TypeIngreso {
				pendingInc += t.Amount
			} else {
				pendingExp += t.Amount
			}
		}
	}
	projected := balance + pendingInc - pendingExp

	// ── Month-over-month net change ───────────────────────────────────────
	cy, cm, _ := now.Date()
	prevTime := now.AddDate(0, -1, 0)
	py, pm, _ := prevTime.Date()
	thisInc, thisExp := monthlyTotals(all, cy, cm)
	prevInc, prevExp := monthlyTotals(all, py, pm)
	thisNet := thisInc - thisExp
	prevNet := prevInc - prevExp
	projectedChange := 0.0
	if prevNet != 0 {
		raw := (thisNet - prevNet) / math.Abs(prevNet) * 100
		projectedChange = math.Round(raw*10) / 10
		if projectedChange > 999 {
			projectedChange = 999
		} else if projectedChange < -999 {
			projectedChange = -999
		}
	}

	// ── Alerts: pending invoices (FV) and purchases (FC) ─────────────────────
	alerts := []domain.Alert{}
	for _, inv := range invoices {
		if inv.Status == domain.StatusCompleted || inv.Status == domain.StatusCancelled {
			continue
		}
		title := "Cobro Pendiente"
		if inv.Status == domain.StatusPartial {
			title = "Cobro Parcial"
		}
		dueDate := inv.DueDate
		if dueDate == "" {
			dueDate = inv.Date
		}
		alerts = append(alerts, domain.Alert{
			ID:          inv.ID,
			Type:        "success",
			Title:       title,
			Description: inv.CustomerName,
			Amount:      inv.Balance,
			DueDate:     dueDate,
		})
	}
	for _, pur := range purchases {
		if pur.Status == domain.StatusCompleted || pur.Status == domain.StatusCancelled {
			continue
		}
		title := "Pago Pendiente"
		if pur.Status == domain.StatusPartial {
			title = "Pago Parcial"
		}
		dueDate := pur.DueDate
		if dueDate == "" {
			dueDate = pur.Date
		}
		alerts = append(alerts, domain.Alert{
			ID:          pur.ID,
			Type:        "danger",
			Title:       title,
			Description: pur.ProviderName,
			Amount:      pur.Balance,
			DueDate:     dueDate,
		})
	}
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].Amount > alerts[j].Amount })
	if len(alerts) > 4 {
		alerts = alerts[:4]
	}

	jsonOK(w, domain.CashFlowSummary{
		Days:             days,
		ProjectedBalance: projected,
		ProjectedChange:  projectedChange,
		Alerts:           alerts,
	})
}
