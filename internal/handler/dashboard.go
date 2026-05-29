package handler

import (
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
	all, err := h.store.GetAllTransactions()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	now := time.Now()
	y, m, _ := now.Date()

	monthInc, monthExp := monthlyTotals(all, y, m)
	balance := monthInc - monthExp

	prevY, prevM, _ := now.AddDate(0, -1, 0).Date()
	prevInc, prevExp := monthlyTotals(all, prevY, prevM)

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
			Title: "SALDO ACTUAL", Value: balance,
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

	// Chart: last 7 months — anchor to 1st to avoid day-overflow (e.g. May 29 - 3mo = Feb 29 → Mar 1 in non-leap years)
	chartData := make([]domain.ChartDataPoint, 7)
	firstOfMonth := time.Date(y, m, 1, 0, 0, 0, 0, now.Location())
	for i := 0; i < 7; i++ {
		t := firstOfMonth.AddDate(0, -(6 - i), 0)
		inc, exp := monthlyTotals(all, t.Year(), t.Month())
		chartData[i] = domain.ChartDataPoint{
			Name:     spanishMonths[t.Month()],
			Ingresos: inc,
			Egresos:  exp,
			Saldo:    inc - exp,
		}
	}

	// Expense pie: top 5 categories by % of total egresos
	catMap := expenseByCategory(all)
	var totalExp float64
	for _, v := range catMap {
		totalExp += v
	}
	pie := []domain.PieSlice{}
	for cat, v := range catMap {
		pie = append(pie, domain.PieSlice{Name: cat, Value: pct(v, totalExp)})
	}
	sort.Slice(pie, func(i, j int) bool { return pie[i].Value > pie[j].Value })
	if len(pie) > 5 {
		pie = pie[:5]
	}

	// Weekly breakdown for the current month (cash basis)
	weekMap := map[int]*domain.WeeklyComparison{}
	for _, t := range all {
		if t.IsProjection || t.Status == domain.StatusCancelled {
			continue
		}
		r := receivedAmount(t)
		if r == 0 {
			continue
		}
		ty, tm, td := txDate(t)
		if ty != y || tm != m {
			continue
		}
		week := (td-1)/7 + 1
		if weekMap[week] == nil {
			weekMap[week] = &domain.WeeklyComparison{Week: week}
		}
		if t.Type == domain.TypeIngreso {
			weekMap[week].Ingresos += r
		} else {
			weekMap[week].Egresos += r
		}
	}
	weeklyData := []domain.WeeklyComparison{}
	for _, wc := range weekMap {
		weeklyData = append(weeklyData, *wc)
	}
	sort.Slice(weeklyData, func(i, j int) bool { return weeklyData[i].Week < weeklyData[j].Week })

	alerts := pendingAlerts(all, now, 5, 4)
	// Prepend a synthetic balance alert when negative
	if balance < 0 {
		alerts = append([]domain.Alert{{
			ID: "balance-warning", Type: "danger",
			Title:       "Saldo Negativo",
			Description: "El saldo actual es negativo. Revise los egresos pendientes.",
			Amount:      -balance,
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

// GET /api/v1/dashboard/bank-balance
func (h *DashboardHandler) GetBankBalance(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.GetBankBalance()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, b) // nil serialises as JSON null → frontend shows '—'
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
	jsonOK(w, b)
}
