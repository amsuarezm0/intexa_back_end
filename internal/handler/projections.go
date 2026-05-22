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

type ProjectionsHandler struct {
	store repository.Store
}

func NewProjectionsHandler(store repository.Store) *ProjectionsHandler {
	return &ProjectionsHandler{store: store}
}

func (h *ProjectionsHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	days := 30
	switch r.URL.Query().Get("days") {
	case "60":
		days = 60
	case "90":
		days = 90
	}

	all, err := h.store.GetAllTransactions()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	now := time.Now()

	balance := currentBalance(all)

	// Build a day-offset map of net flows from pending/projection transactions.
	// Overdue pending items (daysAway < 0) are applied at day 0.
	dailyNet := map[int]float64{}
	var projInc, projExp float64
	for _, t := range all {
		if t.Status == domain.StatusCompleted || t.Status == domain.StatusCancelled {
			continue
		}
		daysAway := int(math.Round(t.CreatedAt.Sub(now).Hours() / 24))
		if daysAway < 0 {
			daysAway = 0
		}
		if daysAway <= days {
			if t.Type == domain.TypeIngreso {
				dailyNet[daysAway] += t.Amount
				projInc += t.Amount
			} else {
				dailyNet[daysAway] -= t.Amount
				projExp += t.Amount
			}
		}
	}

	// Sample the running balance at evenly-spaced checkpoints across the window
	checkpoints := buildCheckpoints(days)
	chartData := make([]domain.ProjectionPoint, len(checkpoints))
	running := balance
	prev := -1
	for i, cp := range checkpoints {
		for d := prev + 1; d <= cp; d++ {
			running += dailyNet[d]
		}
		prev = cp
		label := "HOY"
		if cp > 0 {
			label = fmt.Sprintf("DÍA %d", cp)
		}
		deficit := 0.0
		if running < 0 {
			deficit = -running
		}
		chartData[i] = domain.ProjectionPoint{
			Day:     label,
			Balance: math.Max(running, 0),
			Deficit: deficit,
		}
	}

	// Alerts: pending and projection transactions, most urgent first
	type entry struct {
		alert    domain.ProjectionAlert
		daysAway int
		amount   float64
	}
	var entries []entry
	for _, t := range all {
		if t.Status == domain.StatusCompleted || t.Status == domain.StatusCancelled {
			continue
		}
		daysAway := int(math.Round(t.CreatedAt.Sub(now).Hours() / 24))
		color, icon := "brand-success", "FileCheck"
		if t.Type == domain.TypeEgreso {
			color, icon = "brand-danger", "AlertCircle"
		}
		var dueStr string
		switch {
		case daysAway <= 0:
			dueStr = "Vence hoy"
		case daysAway == 1:
			dueStr = "Vence mañana"
		default:
			dueStr = fmt.Sprintf("Próximo en %d días", daysAway)
		}
		entries = append(entries, entry{
			alert: domain.ProjectionAlert{
				ID: t.ID, Icon: icon,
				Title:       t.Description,
				Description: t.Category,
				DueDate:     dueStr,
				Amount:      formatCOP(t.Amount),
				Color:       color,
			},
			daysAway: daysAway,
			amount:   t.Amount,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].daysAway != entries[j].daysAway {
			return entries[i].daysAway < entries[j].daysAway
		}
		return entries[i].amount > entries[j].amount
	})
	alerts := make([]domain.ProjectionAlert, 0, len(entries))
	for _, e := range entries {
		alerts = append(alerts, e.alert)
	}
	if len(alerts) > 5 {
		alerts = alerts[:5]
	}

	jsonOK(w, domain.ProjectionSummary{
		ChartData:         chartData,
		ProjectedIncome:   projInc,
		ProjectedExpenses: projExp,
		EstimatedBalance:  balance + projInc - projExp,
		Alerts:            alerts,
	})
}

func (h *ProjectionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var t domain.Transaction
	if err := decode(r, &t); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	t.IsProjection = true
	h.store.CreateTransaction(&t)
	jsonCreated(w, t)
}

func (h *ProjectionsHandler) Simulate(w http.ResponseWriter, r *http.Request) {
	var req domain.SimulateRequest
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	all, err := h.store.GetAllTransactions()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	base := currentBalance(all)
	// Include pending flows in base
	for _, t := range all {
		if t.Status != domain.StatusPending || t.IsProjection {
			continue
		}
		if t.Type == domain.TypeIngreso {
			base += t.Amount
		} else {
			base -= t.Amount
		}
	}

	impact := base * (req.SalesGrowth / 100)
	// Each day of payment delay costs ~0.05% of base (working capital friction)
	penalty := float64(req.PaymentDelay) * (math.Abs(base) * 0.0005)
	projected := base + impact - penalty

	risk := "low"
	if projected < base*0.3 {
		risk = "high"
	} else if projected < base*0.6 {
		risk = "medium"
	}

	jsonOK(w, domain.SimulateResponse{
		ProjectedBalance: projected,
		Impact:           impact - penalty,
		RiskLevel:        risk,
	})
}

// buildCheckpoints returns 8 evenly-spaced day indices from 0 to days (inclusive).
func buildCheckpoints(days int) []int {
	n := 8
	pts := make([]int, n)
	for i := 0; i < n; i++ {
		pts[i] = int(math.Round(float64(i) * float64(days) / float64(n-1)))
	}
	return pts
}
