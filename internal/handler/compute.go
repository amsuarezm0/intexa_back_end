package handler

import (
	"fmt"
	"math"
	"time"

	"github.com/intexa/arca-api/internal/domain"
)

var spanishMonths = [13]string{"", "ENE", "FEB", "MAR", "ABR", "MAY", "JUN", "JUL", "AGO", "SEP", "OCT", "NOV", "DIC"}

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

func pct(part, total float64) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(part/total*1000) / 10
}

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

// receivedAmount returns the cash actually received/paid for a completed transaction.
func receivedAmount(t *domain.Transaction) float64 {
	if t.Status == domain.StatusCompleted {
		return t.Amount
	}
	return 0
}

// txDate parses a transaction's accounting Date field (YYYY-MM-DD).
func txDate(t *domain.Transaction) (int, time.Month, int) {
	d, err := time.ParseInLocation("2006-01-02", t.Date, time.Local)
	if err != nil {
		d = t.CreatedAt
	}
	y, m, day := d.Date()
	return y, m, day
}
