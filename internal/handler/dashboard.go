package handler

import (
	"fmt"
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

	balance := currentBalance(all)
	monthInc, monthExp := monthlyTotals(all, y, m)

	prevY, prevM, _ := now.AddDate(0, -1, 0).Date()
	prevInc, prevExp := monthlyTotals(all, prevY, prevM)

	balChange := signedPct(monthInc-monthExp, prevInc-prevExp) + " vs mes ant."
	incChange := signedPct(monthInc, prevInc) + " vs mes ant."
	expChange := signedPct(monthExp, prevExp) + " vs mes ant."

	stats := []domain.StatCard{
		{
			Title: "SALDO ACTUAL", Value: formatCOP(balance),
			Change: balChange, IsPositive: monthInc >= monthExp,
			TrendText: "vs mes ant.", Icon: "Building2",
		},
		{
			Title: "INGRESOS MES", Value: formatCOP(monthInc),
			Change: incChange, IsPositive: monthInc >= prevInc,
			Icon: "ArrowDownCircle",
		},
		{
			Title: "EGRESOS MES", Value: formatCOP(monthExp),
			Change: expChange, IsPositive: monthExp <= prevExp,
			Icon: "ArrowUpCircle",
		},
	}

	// Chart: last 7 months
	chartData := make([]domain.ChartDataPoint, 7)
	for i := 0; i < 7; i++ {
		t := now.AddDate(0, -(6-i), 0)
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

	// Weekly breakdown for the current month
	weekMap := map[int]*domain.WeeklyComparison{}
	for _, t := range all {
		if t.IsProjection {
			continue
		}
		ty, tm, td := t.CreatedAt.Date()
		if ty != y || tm != m {
			continue
		}
		week := (td-1)/7 + 1
		if weekMap[week] == nil {
			weekMap[week] = &domain.WeeklyComparison{Week: week}
		}
		if t.Type == domain.TypeIngreso {
			weekMap[week].Ingresos += t.Amount
		} else {
			weekMap[week].Egresos += t.Amount
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
			Description: fmt.Sprintf("El saldo actual es %s. Revise los egresos pendientes.", formatCOP(balance)),
			Amount:      -balance,
		}}, alerts...)
	}

	jsonOK(w, domain.DashboardSummary{
		Stats:      stats,
		NetFlow:    monthInc - monthExp,
		ChartData:  chartData,
		ExpensePie: pie,
		Alerts:     alerts,
		WeeklyData: weeklyData,
	})
}
