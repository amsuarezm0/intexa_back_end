package handler

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
)

type ReportsHandler struct {
	store repository.Store
}

func NewReportsHandler(store repository.Store) *ReportsHandler {
	return &ReportsHandler{store: store}
}

func (h *ReportsHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "mensual"
	}

	all, err := h.store.GetAllTransactions()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	budgets, err := h.store.GetBudgets()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	now := time.Now()

	var totalMonthlyBudget float64
	budgetByCategory := map[string]float64{}
	for _, b := range budgets {
		budgetByCategory[b.Category] = b.Monthly
		totalMonthlyBudget += b.Monthly
	}

	var chart []domain.ReportDataPoint
	var pie []domain.PieSlice
	var deviationTable []domain.DeviationRow
	var projectedClose, probability float64
	var insight string

	curY, curM, _ := now.Date()
	curQ := (int(curM)-1)/3 + 1

	switch period {

	// ── Trimestral: Q1–Q4 del año en curso ────────────────────────────────────
	case "trimestral":
		chart = make([]domain.ReportDataPoint, 4)
		for q := 1; q <= 4; q++ {
			_, exp := quarterlyTotals(all, curY, q)
			var ej float64
			if q <= curQ {
				ej = exp
			}
			chart[q-1] = domain.ReportDataPoint{
				Name:        fmt.Sprintf("Q%d %d", q, curY),
				Ejecutado:   ej,
				Presupuesto: totalMonthlyBudget * 3,
			}
		}

		// Category breakdown: trimestre en curso
		pie = categoryBreakdownRange(all, quarterStart(curY, curQ), now)

		// Deviation: trimestre en curso vs 3× presupuesto mensual
		deviationTable = periodDeviationTable(all, budgets, quarterStart(curY, curQ), now, 3)

		// Projection: promedio neto trimestral × trimestres restantes
		var sumQ float64
		lookQ := 0
		for i := 1; i <= 4; i++ {
			q := curQ - i
			y := curY
			for q <= 0 {
				q += 4
				y--
			}
			inc, exp := quarterlyTotals(all, y, q)
			if inc+exp > 0 {
				sumQ += inc - exp
				lookQ++
			}
		}
		var avgQ float64
		if lookQ > 0 {
			avgQ = sumQ / float64(lookQ)
		}
		quartersLeft := float64(4 - curQ)
		projectedClose = currentBalance(all) + avgQ*quartersLeft
		probability = complianceRate(all, totalMonthlyBudget, now, 4, "quarter")
		if avgQ > 0 {
			insight = fmt.Sprintf("Promedio neto positivo en los últimos %d trimestres: %s/trimestre.", lookQ, formatCOP(avgQ))
		} else {
			insight = fmt.Sprintf("Promedio neto negativo en los últimos %d trimestres: %s/trimestre. Revisa egresos.", lookQ, formatCOP(avgQ))
		}

	// ── Anual: los 12 meses del año en curso (ENE–DIC) ────────────────────────
	case "anual":
		chart = make([]domain.ReportDataPoint, 12)
		for m := 1; m <= 12; m++ {
			_, exp := monthlyTotals(all, curY, time.Month(m))
			var ej float64
			if time.Month(m) <= curM {
				ej = exp
			}
			chart[m-1] = domain.ReportDataPoint{
				Name:        spanishMonths[time.Month(m)],
				Ejecutado:   ej,
				Presupuesto: totalMonthlyBudget,
			}
		}

		// Category breakdown: año en curso (YTD)
		pie = categoryBreakdownRange(all, time.Date(curY, 1, 1, 0, 0, 0, 0, now.Location()), now)

		// Deviation: año en curso vs presupuesto anual
		deviationTable = periodDeviationTable(all, budgets, time.Date(curY, 1, 1, 0, 0, 0, 0, now.Location()), now, int(curM))

		// Projection: proyección al cierre del año basada en ritmo YTD
		ytdInc, ytdExp := func() (float64, float64) {
			var i, e float64
			for m := 1; m <= int(curM); m++ {
				inc, exp := monthlyTotals(all, curY, time.Month(m))
				i += inc
				e += exp
			}
			return i, e
		}()
		avgMonthNet := (ytdInc - ytdExp) / float64(curM)
		monthsLeft := float64(12 - int(curM))
		projectedClose = currentBalance(all) + avgMonthNet*monthsLeft
		probability = complianceRate(all, totalMonthlyBudget, now, 12, "month")
		if avgMonthNet > 0 {
			insight = fmt.Sprintf("Ritmo mensual positivo en lo que va del año %d: %s/mes promedio.", curY, formatCOP(avgMonthNet))
		} else {
			insight = fmt.Sprintf("Ritmo mensual negativo en lo que va del año %d: %s/mes. Revisa egresos.", curY, formatCOP(avgMonthNet))
		}

	// ── Mensual: últimos 6 meses (rolling) ────────────────────────────────────
	default:
		chart = make([]domain.ReportDataPoint, 6)
		for i := 0; i < 6; i++ {
			t := now.AddDate(0, -(5-i), 0)
			_, exp := monthlyTotals(all, t.Year(), t.Month())
			chart[i] = domain.ReportDataPoint{
				Name:        spanishMonths[t.Month()],
				Ejecutado:   exp,
				Presupuesto: totalMonthlyBudget,
			}
		}

		// Category breakdown: últimos 3 meses
		pie = categoryBreakdownRange(all, now.AddDate(0, -3, 0), now)

		// Deviation: mes en curso
		deviationTable = periodDeviationTable(all, budgets,
			time.Date(curY, curM, 1, 0, 0, 0, 0, now.Location()), now, 1)

		// Projection: promedio mensual × meses restantes del año
		var sumNet float64
		for i := 1; i <= 3; i++ {
			t := now.AddDate(0, -i, 0)
			inc, exp := monthlyTotals(all, t.Year(), t.Month())
			sumNet += inc - exp
		}
		avgMonthlyNet := sumNet / 3
		monthsRemaining := float64(12 - int(curM))
		projectedClose = currentBalance(all) + avgMonthlyNet*monthsRemaining
		probability = complianceRate(all, totalMonthlyBudget, now, 6, "month")
		if avgMonthlyNet > 0 {
			insight = "Flujo neto promedio positivo en los últimos 3 meses: " + formatCOP(avgMonthlyNet) + "/mes."
		} else {
			insight = "Flujo neto promedio negativo en los últimos 3 meses: " + formatCOP(avgMonthlyNet) + "/mes. Revisa egresos."
		}
	}

	jsonOK(w, domain.ReportSummary{
		CashFlowChart:     chart,
		CategoryBreakdown: pie,
		DeviationTable:    deviationTable,
		Annual: domain.AnnualProjection{
			ProjectedClose: projectedClose,
			Probability:    probability,
			InsightText:    insight,
		},
		ComplianceRate: probability,
	})
}

func (h *ReportsHandler) Export(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	jsonOK(w, map[string]string{"message": "export feature requires spreadsheet library integration"})
}

// ── helpers ────────────────────────────────────────────────────────────────────

func quarterStart(year, quarter int) time.Time {
	month := time.Month((quarter-1)*3 + 1)
	return time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
}

// categoryBreakdownRange builds a pie of egresos between from..to.
func categoryBreakdownRange(txs []*domain.Transaction, from, to time.Time) []domain.PieSlice {
	catMap := map[string]float64{}
	var total float64
	for _, t := range txs {
		if t.IsProjection || t.Type != domain.TypeEgreso {
			continue
		}
		if t.CreatedAt.Before(from) || t.CreatedAt.After(to) {
			continue
		}
		catMap[t.Category] += t.Amount
		total += t.Amount
	}
	pie := []domain.PieSlice{}
	for cat, v := range catMap {
		pie = append(pie, domain.PieSlice{Name: cat, Value: pct(v, total)})
	}
	sort.Slice(pie, func(i, j int) bool { return pie[i].Value > pie[j].Value })
	if len(pie) > 6 {
		pie = pie[:6]
	}
	return pie
}

// periodDeviationTable compares actual activity in [from,to] against
// budgetMultiplier × monthly budget per category.
func periodDeviationTable(txs []*domain.Transaction, budgets []domain.BudgetLine, from, to time.Time, budgetMultiplier int) []domain.DeviationRow {
	catInc := map[string]float64{}
	catExp := map[string]float64{}
	for _, t := range txs {
		if t.IsProjection {
			continue
		}
		if t.CreatedAt.Before(from) || t.CreatedAt.After(to) {
			continue
		}
		if t.Type == domain.TypeIngreso {
			catInc[t.Category] += t.Amount
		} else {
			catExp[t.Category] += t.Amount
		}
	}

	seen := map[string]bool{}
	rows := []domain.DeviationRow{}
	for _, b := range budgets {
		seen[b.Category] = true
		periodBudget := b.Monthly * float64(budgetMultiplier)
		actual := catExp[b.Category]
		if actual == 0 {
			actual = catInc[b.Category]
		}
		dev := periodBudget - actual
		rows = append(rows, domain.DeviationRow{
			Category:   b.Category,
			Budget:     periodBudget,
			Actual:     actual,
			Deviation:  dev,
			IsPositive: dev >= 0,
		})
	}
	for cat, v := range catExp {
		if !seen[cat] {
			rows = append(rows, domain.DeviationRow{
				Category: cat, Budget: 0, Actual: v,
				Deviation: -v, IsPositive: false,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Actual > rows[j].Actual })
	return rows
}

// complianceRate returns the % of recent periods where egresos stayed within budget.
func complianceRate(txs []*domain.Transaction, totalMonthlyBudget float64, now time.Time, lookback int, granularity string) float64 {
	compliant := 0
	curQ := (int(now.Month())-1)/3 + 1
	for i := 1; i <= lookback; i++ {
		var exp float64
		var budget float64
		switch granularity {
		case "quarter":
			q := curQ - i
			y := now.Year()
			for q <= 0 {
				q += 4
				y--
			}
			_, exp = quarterlyTotals(txs, y, q)
			budget = totalMonthlyBudget * 3
		default:
			t := now.AddDate(0, -i, 0)
			_, exp = monthlyTotals(txs, t.Year(), t.Month())
			budget = totalMonthlyBudget
		}
		if exp <= budget {
			compliant++
		}
	}
	return pct(float64(compliant), float64(lookback))
}
