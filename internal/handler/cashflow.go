package handler

import (
	"log/slog"
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
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sixDaysAgo := today.AddDate(0, 0, -6)
	horizon30 := today.AddDate(0, 0, 30)

	// Two months of totals (current + previous) for projectedChange
	prevMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -1, 0)
	monthly, err := h.store.GetMonthlyTotals(prevMonthStart, now)
	if err != nil {
		slog.Error("cashflow: GetMonthlyTotals", "err", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Daily totals for 7-day chart
	daily, err := h.store.GetDailyTotals(sixDaysAgo, today)
	if err != nil {
		slog.Error("cashflow: GetDailyTotals", "err", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Current cash balance
	balance, err := h.store.GetCurrentBalance()
	if err != nil {
		slog.Error("cashflow: GetCurrentBalance", "err", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Pending invoices (FV) and purchases (FC)
	invoices, err := h.store.GetPendingInvoices()
	if err != nil {
		slog.Error("cashflow: GetPendingInvoices", "err", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	purchases, err := h.store.GetPendingPurchases()
	if err != nil {
		slog.Error("cashflow: GetPendingPurchases", "err", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Manual projections within 30 days
	projections, err := h.store.GetPendingProjections(horizon30)
	if err != nil {
		slog.Error("cashflow: GetPendingProjections", "err", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// ── 7-day chart ───────────────────────────────────────────────────────────
	// Index daily totals by date string for O(1) lookup
	dailyIdx := make(map[string]domain.DailyTotal, len(daily))
	for _, d := range daily {
		dailyIdx[d.Date] = d
	}
	days := make([]domain.CashFlowDay, 7)
	for i := 0; i < 7; i++ {
		d := sixDaysAgo.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		dt := dailyIdx[key]
		days[i] = domain.CashFlowDay{
			Label:    weekdayES[d.Weekday()],
			Date:     d.Day(),
			Ingresos: dt.Ingresos,
			Egresos:  dt.Egresos,
		}
	}

	// ── Projected balance (balance + pending FV/FC/manual within 30d) ─────────
	parseDate := func(s string) (time.Time, bool) {
		t, err := time.Parse("2006-01-02", s)
		return t, err == nil
	}

	var pendingInc, pendingExp float64
	for _, inv := range invoices {
		for _, inst := range domain.PendingInstallments(inv.Total, inv.Balance, inv.Installments, firstNonEmpty(inv.DueDate, inv.Date)) {
			if d, ok := parseDate(inst.DueDate); ok && !d.After(horizon30) {
				pendingInc += inst.Value
			}
		}
	}
	for _, pur := range purchases {
		for _, inst := range domain.PendingInstallments(pur.Total, pur.Balance, pur.Installments, firstNonEmpty(pur.DueDate, pur.Date)) {
			if d, ok := parseDate(inst.DueDate); ok && !d.After(horizon30) {
				pendingExp += inst.Value
			}
		}
	}
	for _, t := range projections {
		if t.Type == domain.TypeIngreso {
			pendingInc += t.Amount
		} else {
			pendingExp += t.Amount
		}
	}
	projected := balance + pendingInc - pendingExp

	// ── Month-over-month change ───────────────────────────────────────────────
	type mk struct{ y int; m time.Month }
	mIdx := make(map[mk]domain.MonthlyTotal, len(monthly))
	for _, mt := range monthly {
		mIdx[mk{mt.Year, mt.Month}] = mt
	}
	curMT := mIdx[mk{now.Year(), now.Month()}]
	prevMT := mIdx[mk{prevMonthStart.Year(), prevMonthStart.Month()}]
	thisNet := curMT.Income - curMT.Expense
	prevNet := prevMT.Income - prevMT.Expense
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

	// ── Alerts from pending FV (cobros) and FC (pagos) ───────────────────────
	alerts := []domain.Alert{}
	for _, inv := range invoices {
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
		Balance:          balance,
		ProjectedBalance: projected,
		ProjectedChange:  projectedChange,
		Alerts:           alerts,
	})
}

func (h *CashFlowHandler) GetPeriodData(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	dateStr := r.URL.Query().Get("date")

	ref, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		ref = time.Now()
	}
	ref = time.Date(ref.Year(), ref.Month(), ref.Day(), 0, 0, 0, 0, ref.Location())

	var from, to time.Time
	switch period {
	case "day":
		from = ref
		to = ref
	case "week":
		dow := int(ref.Weekday())
		if dow == 0 {
			dow = 7
		}
		from = ref.AddDate(0, 0, -(dow - 1))
		to = from.AddDate(0, 0, 6)
	default: // month
		from = time.Date(ref.Year(), ref.Month(), 1, 0, 0, 0, 0, ref.Location())
		to = time.Date(ref.Year(), ref.Month()+1, 0, 0, 0, 0, 0, ref.Location())
	}

	data, err := h.store.GetPeriodData(from, to)
	if err != nil {
		slog.Error("cashflow: GetPeriodData", "err", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Expose the unpaid installment schedule so the frontend chart can spread
	// pending amounts across their due dates instead of the whole balance.
	for _, inv := range data.Invoices {
		if inv.Status == domain.StatusPending || inv.Status == domain.StatusPartial {
			inv.PendingInstallments = domain.PendingInstallments(inv.Total, inv.Balance, inv.Installments, firstNonEmpty(inv.DueDate, inv.Date))
		}
	}
	for _, pur := range data.Purchases {
		if pur.Status == domain.StatusPending || pur.Status == domain.StatusPartial {
			pur.PendingInstallments = domain.PendingInstallments(pur.Total, pur.Balance, pur.Installments, firstNonEmpty(pur.DueDate, pur.Date))
		}
	}
	jsonOK(w, data)
}
