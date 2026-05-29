package handler

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/intexa/arca-api/internal/domain"
)

var spanishMonths = [13]string{"", "ENE", "FEB", "MAR", "ABR", "MAY", "JUN", "JUL", "AGO", "SEP", "OCT", "NOV", "DIC"}

// formatCOP formats a Colombian peso amount as a compact, human-readable string.
func formatCOP(v float64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	switch {
	case v >= 1_000_000_000:
		return fmt.Sprintf("%s$%.1fB", sign, v/1_000_000_000)
	case v >= 1_000_000:
		return fmt.Sprintf("%s$%.1fM", sign, v/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%s$%.1fK", sign, v/1_000)
	default:
		return fmt.Sprintf("%s$%.0f", sign, v)
	}
}

// pct returns part/total as a percentage rounded to one decimal, or 0 when total is 0.
func pct(part, total float64) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(part/total*1000) / 10
}

// signedPct returns a signed "+X.X%" / "X.X%" string for change indicators.
func signedPct(current, previous float64) string {
	if previous == 0 {
		return ""
	}
	d := (current - previous) / math.Abs(previous) * 100
	if d >= 0 {
		return fmt.Sprintf("+%.1f%%", d)
	}
	return fmt.Sprintf("%.1f%%", d)
}

// currentBalance returns the net of all non-projection transactions (all statuses).
// Mirrors monthlyTotals which also counts all statuses — consistent accrual-basis view.
func currentBalance(txs []*domain.Transaction) float64 {
	var bal float64
	for _, t := range txs {
		if t.IsProjection || t.Status == domain.StatusCancelled {
			continue
		}
		if t.Type == domain.TypeIngreso {
			bal += t.Amount
		} else {
			bal -= t.Amount
		}
	}
	return bal
}

// txDate parses a transaction's accounting Date field (YYYY-MM-DD).
// Falls back to CreatedAt if the field is malformed.
func txDate(t *domain.Transaction) (int, time.Month, int) {
	d, err := time.Parse("2006-01-02", t.Date)
	if err != nil {
		d = t.CreatedAt
	}
	y, m, day := d.Date()
	return y, m, day
}

// monthlyTotals sums ingresos and egresos for a given year/month (non-projections only).
func monthlyTotals(txs []*domain.Transaction, year int, month time.Month) (income, expense float64) {
	for _, t := range txs {
		if t.IsProjection {
			continue
		}
		y, m, _ := txDate(t)
		if y != year || m != month {
			continue
		}
		if t.Type == domain.TypeIngreso {
			income += t.Amount
		} else {
			expense += t.Amount
		}
	}
	return
}

// quarterlyTotals sums ingresos and egresos for a quarter (1–4) of a given year.
func quarterlyTotals(txs []*domain.Transaction, year, quarter int) (income, expense float64) {
	start := time.Month((quarter-1)*3 + 1)
	end := start + 2
	for _, t := range txs {
		if t.IsProjection {
			continue
		}
		y, m, _ := txDate(t)
		if y != year || m < start || m > end {
			continue
		}
		if t.Type == domain.TypeIngreso {
			income += t.Amount
		} else {
			expense += t.Amount
		}
	}
	return
}

// yearlyTotals sums ingresos and egresos for a full calendar year.
func yearlyTotals(txs []*domain.Transaction, year int) (income, expense float64) {
	for _, t := range txs {
		if t.IsProjection {
			continue
		}
		y, _, _ := txDate(t)
		if y != year {
			continue
		}
		if t.Type == domain.TypeIngreso {
			income += t.Amount
		} else {
			expense += t.Amount
		}
	}
	return
}

// expenseByCategory sums egresos grouped by category (non-projections).
func expenseByCategory(txs []*domain.Transaction) map[string]float64 {
	m := map[string]float64{}
	for _, t := range txs {
		if t.IsProjection || t.Type != domain.TypeEgreso {
			continue
		}
		m[t.Category] += t.Amount
	}
	return m
}

// incomeByCategory sums ingresos grouped by category (non-projections).
func incomeByCategory(txs []*domain.Transaction) map[string]float64 {
	m := map[string]float64{}
	for _, t := range txs {
		if t.IsProjection || t.Type != domain.TypeIngreso {
			continue
		}
		m[t.Category] += t.Amount
	}
	return m
}

// pendingAlerts returns Alert items for pending transactions that are >= minAgeDays old,
// sorted by amount descending, capped at maxCount.
func pendingAlerts(txs []*domain.Transaction, now time.Time, minAgeDays, maxCount int) []domain.Alert {
	alerts := []domain.Alert{}
	for _, t := range txs {
		if t.Status != domain.StatusPending || t.IsProjection {
			continue
		}
		age := now.Sub(t.CreatedAt)
		if int(age.Hours()/24) < minAgeDays {
			continue
		}
		kind := "warning"
		if age > 10*24*time.Hour {
			kind = "danger"
		}
		alerts = append(alerts, domain.Alert{
			ID:          t.ID,
			Type:        kind,
			Title:       t.Description,
			Description: fmt.Sprintf("Pendiente hace %d días · %s", int(age.Hours()/24), t.Category),
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
