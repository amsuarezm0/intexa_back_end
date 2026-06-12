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

	now := time.Now()
	curY, curM, _ := now.Date()
	curQ := (int(curM)-1)/3 + 1

	// Fetch current cash balance and two years of monthly totals (covers all lookbacks).
	balance, err := h.store.GetCurrentBalance()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	twoYearsAgo := time.Date(curY-2, curM, 1, 0, 0, 0, 0, now.Location())
	monthly, err := h.store.GetMonthlyTotals(twoYearsAgo, now)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Index monthly totals for O(1) lookup.
	mIdx := make(map[monthKey]domain.MonthlyTotal, len(monthly))
	for _, mt := range monthly {
		mIdx[monthKey{mt.Year, mt.Month}] = mt
	}
	lookupMonth := func(y int, m time.Month) (float64, float64) {
		mt := mIdx[monthKey{y, m}]
		return mt.Income, mt.Expense
	}
	lookupQuarter := func(y, q int) (float64, float64) {
		start := time.Month((q-1)*3 + 1)
		var inc, exp float64
		for i := time.Month(0); i < 3; i++ {
			mo := start + i
			yr := y
			for mo > 12 {
				mo -= 12
				yr++
			}
			i2, e2 := lookupMonth(yr, mo)
			inc += i2
			exp += e2
		}
		return inc, exp
	}

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
				inc, exp = lookupQuarter(curY, q)
			}
			chart[q-1] = domain.ReportDataPoint{
				Name:     fmt.Sprintf("Q%d %d", q, curY),
				Ingresos: inc,
				Egresos:  exp,
			}
		}

		qStart := quarterStart(curY, curQ)
		currCat, err := h.store.GetCategoryTotals(qStart, now, domain.TypeEgreso)
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		pie = buildPieSlices(currCat)

		prevQStart := quarterStart(curY, curQ-1)
		prevCat, err := h.store.GetCategoryTotals(prevQStart, qStart, domain.TypeEgreso)
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		categoryTable = buildCategoryTable(currCat, prevCat)

		var sumQ float64
		lookQ := 0
		for i := 1; i <= 4; i++ {
			q := curQ - i
			y := curY
			for q <= 0 {
				q += 4
				y--
			}
			inc, exp := lookupQuarter(y, q)
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
		projectedClose = balance + avgQ*quartersLeft
		probability = netPositiveRate(mIdx, 4, "quarter", now)
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
				inc, exp = lookupMonth(curY, time.Month(m))
			}
			chart[m-1] = domain.ReportDataPoint{
				Name:     spanishMonths[time.Month(m)],
				Ingresos: inc,
				Egresos:  exp,
			}
		}

		yearStart := time.Date(curY, 1, 1, 0, 0, 0, 0, now.Location())
		currCat, err := h.store.GetCategoryTotals(yearStart, now, domain.TypeEgreso)
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		pie = buildPieSlices(currCat)

		prevYearStart := time.Date(curY-1, 1, 1, 0, 0, 0, 0, now.Location())
		prevYearEnd := now.AddDate(-1, 0, 0)
		prevCat, err := h.store.GetCategoryTotals(prevYearStart, prevYearEnd, domain.TypeEgreso)
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		categoryTable = buildCategoryTable(currCat, prevCat)

		var ytdInc, ytdExp float64
		for m := 1; m <= int(curM); m++ {
			inc, exp := lookupMonth(curY, time.Month(m))
			ytdInc += inc
			ytdExp += exp
		}
		avgMonthNet := (ytdInc - ytdExp) / float64(curM)
		monthsLeft := float64(12 - int(curM))
		projectedClose = balance + avgMonthNet*monthsLeft
		probability = netPositiveRate(mIdx, 12, "month", now)
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
			inc, exp := lookupMonth(t.Year(), t.Month())
			chart[i] = domain.ReportDataPoint{
				Name:     spanishMonths[t.Month()],
				Ingresos: inc,
				Egresos:  exp,
			}
		}

		sixMonthsAgo := now.AddDate(0, -5, 0)
		sixMonthStart := time.Date(sixMonthsAgo.Year(), sixMonthsAgo.Month(), 1, 0, 0, 0, 0, now.Location())
		currCat, err := h.store.GetCategoryTotals(sixMonthStart, now, domain.TypeEgreso)
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		pie = buildPieSlices(currCat)

		prevSixMonthsAgo := now.AddDate(0, -11, 0)
		prevSixMonthStart := time.Date(prevSixMonthsAgo.Year(), prevSixMonthsAgo.Month(), 1, 0, 0, 0, 0, now.Location())
		prevCat, err := h.store.GetCategoryTotals(prevSixMonthStart, sixMonthStart, domain.TypeEgreso)
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		categoryTable = buildCategoryTable(currCat, prevCat)

		var sumNet float64
		for i := 1; i <= 3; i++ {
			t := now.AddDate(0, -i, 0)
			inc, exp := lookupMonth(t.Year(), t.Month())
			sumNet += inc - exp
		}
		avgMonthlyNet := sumNet / 3
		monthsRemaining := float64(12 - int(curM))
		projectedClose = balance + avgMonthlyNet*monthsRemaining
		probability = netPositiveRate(mIdx, 6, "month", now)
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

type monthKey struct{ y int; m time.Month }

func quarterStart(year, quarter int) time.Time {
	for quarter <= 0 {
		quarter += 4
		year--
	}
	month := time.Month((quarter-1)*3 + 1)
	return time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
}

func buildPieSlices(totals []domain.CategoryTotal) []domain.PieSlice {
	var total float64
	for _, ct := range totals {
		total += ct.Amount
	}
	pie := make([]domain.PieSlice, 0, len(totals))
	for _, ct := range totals {
		if p := pct(ct.Amount, total); p > 0 {
			pie = append(pie, domain.PieSlice{Name: ct.Category, Value: p})
		}
	}
	// Adjust rounding so slices sum to 100
	if len(pie) > 0 {
		var sum float64
		for _, s := range pie {
			sum += s.Value
		}
		pie[0].Value = math.Round((pie[0].Value+(100-sum))*10) / 10
	}
	return pie
}

func buildCategoryTable(curr, prev []domain.CategoryTotal) []domain.CategoryRow {
	prevMap := make(map[string]float64, len(prev))
	for _, ct := range prev {
		prevMap[ct.Category] = ct.Amount
	}
	rows := make([]domain.CategoryRow, 0, len(curr))
	for _, ct := range curr {
		p := prevMap[ct.Category]
		var change float64
		if p > 0 {
			change = math.Round(((ct.Amount-p)/p)*1000) / 10
		}
		rows = append(rows, domain.CategoryRow{
			Category:   ct.Category,
			Amount:     ct.Amount,
			Prev:       p,
			Change:     change,
			IsPositive: ct.Amount <= p || p == 0,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Amount > rows[j].Amount })
	return rows
}

func netPositiveRate(mIdx map[monthKey]domain.MonthlyTotal, lookback int, granularity string, now time.Time) float64 {
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
			start := time.Month((q-1)*3 + 1)
			for j := time.Month(0); j < 3; j++ {
				mo := start + j
				yr := y
				for mo > 12 {
					mo -= 12
					yr++
				}
				mt := mIdx[monthKey{yr, mo}]
				inc += mt.Income
				exp += mt.Expense
			}
		default:
			t := now.AddDate(0, -i, 0)
			mt := mIdx[monthKey{t.Year(), t.Month()}]
			inc, exp = mt.Income, mt.Expense
		}
		if inc >= exp {
			positive++
		}
	}
	return pct(float64(positive), float64(lookback))
}
