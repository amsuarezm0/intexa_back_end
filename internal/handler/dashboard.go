package handler

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
)

type DashboardHandler struct {
	store repository.Store
}

func NewDashboardHandler(store repository.Store) *DashboardHandler {
	return &DashboardHandler{store: store}
}

func (h *DashboardHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	y, m, _ := now.Date()
	firstOfMonth := time.Date(y, m, 1, 0, 0, 0, 0, now.Location())
	prevMonthStart := firstOfMonth.AddDate(0, -1, 0)

	// 6 months of monthly totals for chart + stats (current month included)
	sixMonthsAgo := time.Date(y, m, 1, 0, 0, 0, 0, now.Location()).AddDate(0, -5, 0)
	monthly, err := h.store.GetMonthlyTotals(sixMonthsAgo, now)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Expense categories for current month
	catTotals, err := h.store.GetCategoryTotals(firstOfMonth, now, domain.TypeEgreso)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Weekly breakdown for current month
	weeklyData, err := h.store.GetWeeklyTotals(y, m)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Pending manual transactions for alerts
	pending, err := h.store.GetPendingTransactions()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Index monthly totals by year+month for O(1) lookup
	type mk struct{ y int; m time.Month }
	mIdx := make(map[mk]domain.MonthlyTotal, len(monthly))
	for _, mt := range monthly {
		mIdx[mk{mt.Year, mt.Month}] = mt
	}
	lookup := func(yr int, mo time.Month) (float64, float64) {
		mt := mIdx[mk{yr, mo}]
		return mt.Income, mt.Expense
	}

	monthInc, monthExp := lookup(y, m)
	prevInc, prevExp := lookup(prevMonthStart.Year(), prevMonthStart.Month())

	balChange := signedPct(monthInc-monthExp, prevInc-prevExp)
	incChange := signedPct(monthInc, prevInc)
	expChange := signedPct(monthExp, prevExp)

	trendLabel := func(change string) string {
		if change == "" {
			return "Sin datos mes anterior"
		}
		return "vs mes ant."
	}

	stats := []domain.StatCard{
		{
			Title: "SALDO ACTUAL", Value: monthInc - monthExp,
			Change: balChange, IsPositive: monthInc >= monthExp,
			TrendText: trendLabel(balChange), Icon: "Building2",
		},
		{
			Title: "INGRESOS MES", Value: monthInc,
			Change: incChange, IsPositive: monthInc >= prevInc,
			TrendText: trendLabel(incChange), Icon: "ArrowDownCircle",
		},
		{
			Title: "EGRESOS MES", Value: monthExp,
			Change: expChange, IsPositive: monthExp <= prevExp,
			TrendText: trendLabel(expChange), Icon: "ArrowUpCircle",
		},
	}

	// Chart: 6 months ending on current month
	chartData := make([]domain.ChartDataPoint, 6)
	for i := 0; i < 6; i++ {
		t := sixMonthsAgo.AddDate(0, i, 0)
		inc, exp := lookup(t.Year(), t.Month())
		chartData[i] = domain.ChartDataPoint{
			Name:     spanishMonths[t.Month()],
			Ingresos: inc,
			Egresos:  exp,
			Saldo:    inc - exp,
		}
	}

	// Expense pie: all categories by % of total (current month)
	var totalExp float64
	for _, ct := range catTotals {
		totalExp += ct.Amount
	}
	pie := make([]domain.PieSlice, 0, len(catTotals))
	for _, ct := range catTotals {
		if p := pct(ct.Amount, totalExp); p > 0 {
			pie = append(pie, domain.PieSlice{Name: ct.Category, Value: p})
		}
	}
	if len(pie) > 0 {
		var sum float64
		for _, s := range pie {
			sum += s.Value
		}
		pie[0].Value = math.Round((pie[0].Value+(100-sum))*10) / 10
	}

	// Alerts from pending manual transactions (overdue >= 5 days)
	alerts := buildPendingAlerts(pending, now, 5, 4)
	if monthInc-monthExp < 0 {
		alerts = append([]domain.Alert{{
			ID: "balance-warning", Type: "danger",
			Title:       "Saldo Negativo",
			Description: "El saldo actual es negativo. Revise los egresos pendientes.",
			Amount:      -(monthInc - monthExp),
		}}, alerts...)
	}

	jsonOK(w, domain.DashboardSummary{
		Stats:        stats,
		NetFlow:      monthInc - monthExp,
		MonthIncome:  monthInc,
		MonthExpense: monthExp,
		ChartData:    chartData,
		ExpensePie:   pie,
		Alerts:       alerts,
		WeeklyData:   weeklyData,
	})
}

// buildPendingAlerts builds Alert items from pending (non-projection) transactions
// that are >= minAgeDays overdue.
func buildPendingAlerts(pending []*domain.Transaction, now time.Time, minAgeDays, maxCount int) []domain.Alert {
	alerts := []domain.Alert{}
	for _, t := range pending {
		d, err := time.Parse("2006-01-02", t.Date)
		if err != nil {
			d = t.CreatedAt
		}
		ageDays := int(now.Sub(d).Hours() / 24)
		if ageDays < minAgeDays {
			continue
		}
		kind := "warning"
		if ageDays > 10 {
			kind = "danger"
		}
		alerts = append(alerts, domain.Alert{
			ID:          t.ID,
			Type:        kind,
			Title:       t.Description,
			Description: fmt.Sprintf("Pendiente hace %d días · %s", ageDays, t.Category),
			Amount:      t.Amount,
			DueDate:     t.Date,
		})
	}
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].Amount > alerts[j].Amount })
	if len(alerts) > maxCount {
		return alerts[:maxCount]
	}
	return alerts
}

// GET /api/v1/dashboard/bank-balance
func (h *DashboardHandler) GetBankBalance(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.GetBankBalance()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, b)
}

// PUT /api/v1/dashboard/bank-balance
func (h *DashboardHandler) UpdateBankBalance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	updatedBy, _ := actorFrom(r)

	b := domain.BankBalance{
		Amount:    req.Amount,
		UpdatedAt: time.Now(),
		UpdatedBy: updatedBy,
	}
	if err := h.store.SetBankBalance(b); err != nil {
		jsonError(w, "failed to save bank balance", http.StatusInternalServerError)
		return
	}
	_, initial := actorFrom(r)
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: updatedBy, Initial: initial, Action: "Actualizó saldo bancario",
		Module: "Dashboard", Color: "bg-blue-500",
	})
	jsonOK(w, b)
}
