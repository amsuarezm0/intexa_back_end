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

// receivedAmount returns the cash actually received/paid for t on a cash basis.
// Transactions use only Completado / Pendiente / Anulado — no partial balance.
func receivedAmount(t *domain.Transaction) float64 {
	if t.Status == domain.StatusCompleted {
		return t.Amount
	}
	return 0
}

// pendingAmount returns the cash still owed/expected from t.
// For transactions this is non-zero only for Pendiente status (manual projections).
func pendingAmount(t *domain.Transaction) float64 {
	if t.Status == domain.StatusPending {
		return t.Amount
	}
	return 0
}

// currentBalance returns the net cash actually received to date (cash basis).
// Completado counts at full Amount; Parcial at Amount−Balance; Pendiente counts 0.
func currentBalance(txs []*domain.Transaction) float64 {
	var bal float64
	for _, t := range txs {
		if t.IsProjection || t.Status == domain.StatusCancelled {
			continue
		}
		r := receivedAmount(t)
		if r == 0 {
			continue
		}
		if t.Type == domain.TypeIngreso {
			bal += r
		} else {
			bal -= r
		}
	}
	return bal
}

// txDate parses a transaction's accounting Date field (YYYY-MM-DD).
// Falls back to CreatedAt if the field is malformed.
func txDate(t *domain.Transaction) (int, time.Month, int) {
	d, err := time.ParseInLocation("2006-01-02", t.Date, time.Local)
	if err != nil {
		d = t.CreatedAt
	}
	y, m, day := d.Date()
	return y, m, day
}

// monthlyTotals sums cash received/paid for a given year/month (cash basis, non-projections).
// Completado counts at full Amount; Parcial at Amount−Balance; Pendiente counts 0.
func monthlyTotals(txs []*domain.Transaction, year int, month time.Month) (income, expense float64) {
	for _, t := range txs {
		if t.IsProjection || t.Status == domain.StatusCancelled {
			continue
		}
		r := receivedAmount(t)
		if r == 0 {
			continue
		}
		y, m, _ := txDate(t)
		if y != year || m != month {
			continue
		}
		if t.Type == domain.TypeIngreso {
			income += r
		} else {
			expense += r
		}
	}
	return
}

// quarterlyTotals sums cash received/paid for a quarter (1–4) of a given year (cash basis).
func quarterlyTotals(txs []*domain.Transaction, year, quarter int) (income, expense float64) {
	start := time.Month((quarter-1)*3 + 1)
	end := start + 2
	for _, t := range txs {
		if t.IsProjection || t.Status == domain.StatusCancelled {
			continue
		}
		r := receivedAmount(t)
		if r == 0 {
			continue
		}
		y, m, _ := txDate(t)
		if y != year || m < start || m > end {
			continue
		}
		if t.Type == domain.TypeIngreso {
			income += r
		} else {
			expense += r
		}
	}
	return
}

// yearlyTotals sums cash received/paid for a full calendar year (cash basis).
func yearlyTotals(txs []*domain.Transaction, year int) (income, expense float64) {
	for _, t := range txs {
		if t.IsProjection || t.Status == domain.StatusCancelled {
			continue
		}
		r := receivedAmount(t)
		if r == 0 {
			continue
		}
		y, _, _ := txDate(t)
		if y != year {
			continue
		}
		if t.Type == domain.TypeIngreso {
			income += r
		} else {
			expense += r
		}
	}
	return
}

// expenseByCategory sums cash paid grouped by category (cash basis, non-projections).
func expenseByCategory(txs []*domain.Transaction) map[string]float64 {
	m := map[string]float64{}
	for _, t := range txs {
		if t.IsProjection || t.Status == domain.StatusCancelled || t.Type != domain.TypeEgreso {
			continue
		}
		r := receivedAmount(t)
		if r > 0 {
			m[t.Category] += r
		}
	}
	return m
}

// incomeByCategory sums cash received grouped by category (cash basis, non-projections).
func incomeByCategory(txs []*domain.Transaction) map[string]float64 {
	m := map[string]float64{}
	for _, t := range txs {
		if t.IsProjection || t.Status == domain.StatusCancelled || t.Type != domain.TypeIngreso {
			continue
		}
		r := receivedAmount(t)
		if r > 0 {
			m[t.Category] += r
		}
	}
	return m
}

// pendingAlerts returns Alert items for Pendiente and Parcial transactions that are
// >= minAgeDays old, sorted by outstanding amount descending, capped at maxCount.
// Age is measured from the transaction's accounting date (t.Date), not CreatedAt.
func pendingAlerts(txs []*domain.Transaction, now time.Time, minAgeDays, maxCount int) []domain.Alert {
	alerts := []domain.Alert{}
	for _, t := range txs {
		if t.IsProjection {
			continue
		}
		owed := pendingAmount(t)
		if owed == 0 {
			continue
		}
		txY, txM, txD := txDate(t)
		txDay := time.Date(txY, txM, txD, 0, 0, 0, 0, now.Location())
		ageDays := int(now.Sub(txDay).Hours() / 24)
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
			Amount:      owed,
			DueDate:     t.Date,
		})
	}
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].Amount > alerts[j].Amount })
	if len(alerts) > maxCount {
		return alerts[:maxCount]
	}
	return alerts
}
