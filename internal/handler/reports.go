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
	now := time.Now()
	curY, curM, _ := now.Date()
	curQ := (int(curM)-1)/3 + 1

	var chart []domain.ReportDataPoint
	var pie []domain.PieSlice
	var categoryTable []domain.CategoryRow
	var projectedClose, probability float64
	var insight string

	switch period {

	// ── Trimestral: Q1–Q4 del año en curso ───────────────────────────────────
	case "trimestral":
		chart = make([]domain.ReportDataPoint, 4)
		for q := 1; q <= 4; q++ {
			var inc, exp float64
			if q <= curQ {
				inc, exp = quarterlyTotals(all, curY, q)
			}
			chart[q-1] = domain.ReportDataPoint{
				Name:     fmt.Sprintf("Q%d %d", q, curY),
				Ingresos: inc,
				Egresos:  exp,
			}
		}
		pie = categoryBreakdownRange(all, quarterStart(curY, curQ), now)
		categoryTable = categoryComparisonTable(all, quarterStart(curY, curQ), now,
			quarterStart(curY, curQ-1), quarterStart(curY, curQ))

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
		probability = netPositiveRate(all, 4, "quarter", now)
		if avgQ >= 0 {
			insight = fmt.Sprintf("Promedio neto positivo en los últimos %d trimestres: %s/trimestre.", lookQ, formatCOP(avgQ))
		} else {
			insight = fmt.Sprintf("Promedio neto negativo en los últimos %d trimestres: %s/trimestre. Revisa egresos.", lookQ, formatCOP(avgQ))
		}

	// ── Anual: los 12 meses del año en curso ─────────────────────────────────
	case "anual":
		chart = make([]domain.ReportDataPoint, 12)
		for m := 1; m <= 12; m++ {
			var inc, exp float64
			if time.Month(m) <= curM {
				inc, exp = monthlyTotals(all, curY, time.Month(m))
			}
			chart[m-1] = domain.ReportDataPoint{
				Name:     spanishMonths[time.Month(m)],
				Ingresos: inc,
				Egresos:  exp,
			}
		}
		pie = categoryBreakdownRange(all, time.Date(curY, 1, 1, 0, 0, 0, 0, now.Location()), now)
		prevYearStart := time.Date(curY-1, 1, 1, 0, 0, 0, 0, now.Location())
		prevYearEnd := now.AddDate(-1, 0, 0)
		categoryTable = categoryComparisonTable(all,
			time.Date(curY, 1, 1, 0, 0, 0, 0, now.Location()), now,
			prevYearStart, prevYearEnd)

		var ytdInc, ytdExp float64
		for m := 1; m <= int(curM); m++ {
			inc, exp := monthlyTotals(all, curY, time.Month(m))
			ytdInc += inc
			ytdExp += exp
		}
		avgMonthNet := (ytdInc - ytdExp) / float64(curM)
		monthsLeft := float64(12 - int(curM))
		projectedClose = currentBalance(all) + avgMonthNet*monthsLeft
		probability = netPositiveRate(all, 12, "month", now)
		if avgMonthNet >= 0 {
			insight = fmt.Sprintf("Ritmo mensual positivo en lo que va del año %d: %s/mes promedio.", curY, formatCOP(avgMonthNet))
		} else {
			insight = fmt.Sprintf("Ritmo mensual negativo en lo que va del año %d: %s/mes. Revisa egresos.", curY, formatCOP(avgMonthNet))
		}

	// ── Mensual: últimos 6 meses (rolling) ───────────────────────────────────
	default:
		chart = make([]domain.ReportDataPoint, 6)
		for i := 0; i < 6; i++ {
			t := now.AddDate(0, -(5 - i), 0)
			inc, exp := monthlyTotals(all, t.Year(), t.Month())
			chart[i] = domain.ReportDataPoint{
				Name:     spanishMonths[t.Month()],
				Ingresos: inc,
				Egresos:  exp,
			}
		}
		sixMonthsAgo := now.AddDate(0, -5, 0)
		sixMonthStart := time.Date(sixMonthsAgo.Year(), sixMonthsAgo.Month(), 1, 0, 0, 0, 0, now.Location())
		prevSixMonthsAgo := now.AddDate(0, -11, 0)
		prevSixMonthStart := time.Date(prevSixMonthsAgo.Year(), prevSixMonthsAgo.Month(), 1, 0, 0, 0, 0, now.Location())
		pie = categoryBreakdownRange(all, sixMonthStart, now)
		categoryTable = categoryComparisonTable(all, sixMonthStart, now, prevSixMonthStart, sixMonthStart)

		var sumNet float64
		for i := 1; i <= 3; i++ {
			t := now.AddDate(0, -i, 0)
			inc, exp := monthlyTotals(all, t.Year(), t.Month())
			sumNet += inc - exp
		}
		avgMonthlyNet := sumNet / 3
		monthsRemaining := float64(12 - int(curM))
		projectedClose = currentBalance(all) + avgMonthlyNet*monthsRemaining
		probability = netPositiveRate(all, 6, "month", now)
		if avgMonthlyNet >= 0 {
			insight = "Flujo neto promedio positivo en los últimos 3 meses: " + formatCOP(avgMonthlyNet) + "/mes."
		} else {
			insight = "Flujo neto promedio negativo en los últimos 3 meses: " + formatCOP(avgMonthlyNet) + "/mes. Revisa egresos."
		}
	}

	jsonOK(w, domain.ReportSummary{
		CashFlowChart:     chart,
		CategoryBreakdown: pie,
		CategoryTable:     categoryTable,
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

// ── helpers ───────────────────────────────────────────────────────────────────

func quarterStart(year, quarter int) time.Time {
	for quarter <= 0 {
		quarter += 4
		year--
	}
	month := time.Month((quarter-1)*3 + 1)
	return time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
}

func categoryBreakdownRange(txs []*domain.Transaction, from, to time.Time) []domain.PieSlice {
	catMap := map[string]float64{}
	var total float64
	for _, t := range txs {
		if t.IsProjection || t.Status == domain.StatusCancelled || t.Type != domain.TypeEgreso {
			continue
		}
		r := receivedAmount(t)
		if r == 0 {
			continue
		}
		d, err := time.Parse("2006-01-02", t.Date)
		if err != nil {
			d = t.CreatedAt
		}
		if d.Before(from) || d.After(to) {
			continue
		}
		catMap[t.Category] += r
		total += r
	}
	pie := []domain.PieSlice{}
	for cat, v := range catMap {
		if p := pct(v, total); p > 0 {
			pie = append(pie, domain.PieSlice{Name: cat, Value: p})
		}
	}
	sort.Slice(pie, func(i, j int) bool { return pie[i].Value > pie[j].Value })
	if len(pie) > 0 {
		var sum float64
		for _, s := range pie {
			sum += s.Value
		}
		pie[0].Value = math.Round((pie[0].Value+(100-sum))*10) / 10
	}
	return pie
}

// categoryComparisonTable builds per-category cash paid for [from,to] vs [prevFrom,prevTo].
func categoryComparisonTable(txs []*domain.Transaction, from, to, prevFrom, prevTo time.Time) []domain.CategoryRow {
	curr := map[string]float64{}
	prev := map[string]float64{}
	for _, t := range txs {
		if t.IsProjection || t.Status == domain.StatusCancelled || t.Type != domain.TypeEgreso {
			continue
		}
		r := receivedAmount(t)
		if r == 0 {
			continue
		}
		d, err := time.Parse("2006-01-02", t.Date)
		if err != nil {
			d = t.CreatedAt
		}
		if !d.Before(from) && !d.After(to) {
			curr[t.Category] += r
		}
		if !d.Before(prevFrom) && !d.After(prevTo) {
			prev[t.Category] += r
		}
	}
	rows := []domain.CategoryRow{}
	for cat, amount := range curr {
		p := prev[cat]
		var change float64
		if p > 0 {
			change = math.Round(((amount-p)/p)*1000) / 10 // 1 decimal
		}
		rows = append(rows, domain.CategoryRow{
			Category:   cat,
			Amount:     amount,
			Prev:       p,
			Change:     change,
			IsPositive: amount <= p || p == 0,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Amount > rows[j].Amount })
	return rows
}

// netPositiveRate returns the % of recent periods where net flow was positive.
func netPositiveRate(txs []*domain.Transaction, lookback int, granularity string, now time.Time) float64 {
	curQ := (int(now.Month())-1)/3 + 1
	positive := 0
	for i := 1; i <= lookback; i++ {
		var inc, exp float64
		switch granularity {
		case "quarter":
			q := curQ - i
			y := now.Year()
			for q <= 0 {
				q += 4
				y--
			}
			inc, exp = quarterlyTotals(txs, y, q)
		default:
			t := now.AddDate(0, -i, 0)
			inc, exp = monthlyTotals(txs, t.Year(), t.Month())
		}
		if inc >= exp {
			positive++
		}
	}
	return pct(float64(positive), float64(lookback))
}
