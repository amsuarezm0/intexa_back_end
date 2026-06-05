package handler

import (
	"fmt"
	"log/slog"
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

	now := time.Now().Truncate(24 * time.Hour)
	horizon := now.AddDate(0, 0, days)

	balance, err := h.store.GetCurrentBalance()
	if err != nil {
		slog.Error("projections/GetSummary: GetCurrentBalance", "err", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	invoices, err := h.store.GetPendingInvoices()
	if err != nil {
		slog.Error("projections/GetSummary: GetPendingInvoices", "err", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	purchases, err := h.store.GetPendingPurchases()
	if err != nil {
		slog.Error("projections/GetSummary: GetPendingPurchases", "err", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	all, err := h.store.GetPendingProjections(horizon)
	if err != nil {
		slog.Error("projections/GetSummary: GetPendingProjections", "err", err)
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	dailyNet := map[int]float64{}
	var projInc, projExp float64

	addFlow := func(txType domain.TransactionType, amount float64, daysAway int) {
		if txType == domain.TypeIngreso {
			dailyNet[daysAway] += amount
			projInc += amount
		} else {
			dailyNet[daysAway] -= amount
			projExp += amount
		}
	}

	parseDaysAway := func(dateStr string) (int, bool) {
		d, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return 0, false
		}
		n := int(math.Round(d.Sub(now).Hours() / 24))
		if n < 0 {
			n = 0
		}
		return n, true
	}

	// Pending/partial invoices (FV) → income projections.
	for _, inv := range invoices {
		if inv.Status == domain.StatusCompleted || inv.Status == domain.StatusCancelled {
			continue
		}
		dateStr := inv.DueDate
		if dateStr == "" {
			dateStr = inv.Date
		}
		daysAway, ok := parseDaysAway(dateStr)
		if !ok || daysAway > days {
			continue
		}
		addFlow(domain.TypeIngreso, inv.Balance, daysAway)
	}

	// Pending/partial purchases (FC) → expense projections.
	for _, pur := range purchases {
		if pur.Status == domain.StatusCompleted || pur.Status == domain.StatusCancelled {
			continue
		}
		dateStr := pur.DueDate
		if dateStr == "" {
			dateStr = pur.Date
		}
		daysAway, ok := parseDaysAway(dateStr)
		if !ok || daysAway > days {
			continue
		}
		addFlow(domain.TypeEgreso, pur.Balance, daysAway)
	}

	// Manual projections in transactions (is_projection=true).
	for _, t := range all {
		if !t.IsProjection || t.Status == domain.StatusCancelled {
			continue
		}
		daysAway, ok := parseDaysAway(t.Date)
		if !ok || daysAway > days {
			continue
		}
		addFlow(t.Type, t.Amount, daysAway)
	}

	// Build the running-balance chart.
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

	// Build alerts — invoices first (income), then purchases (expense), then manual projections.
	type entry struct {
		alert    domain.ProjectionAlert
		daysAway int
		amount   float64
	}
	var entries []entry

	for _, inv := range invoices {
		if inv.Status == domain.StatusCompleted || inv.Status == domain.StatusCancelled {
			continue
		}
		dateStr := inv.DueDate
		if dateStr == "" {
			dateStr = inv.Date
		}
		daysAway, ok := parseDaysAway(dateStr)
		if !ok || daysAway > days {
			continue
		}
		desc := inv.CustomerName
		if desc == "" {
			desc = inv.Category
		}
		entries = append(entries, entry{
			alert: domain.ProjectionAlert{
				ID:          inv.ID,
				Icon:        "FileCheck",
				Title:       inv.Detail,
				Description: desc,
				DueDate:     dateStr,
				Amount:      inv.Balance,
				Color:       "brand-success",
			},
			daysAway: daysAway,
			amount:   inv.Balance,
		})
	}

	for _, pur := range purchases {
		if pur.Status == domain.StatusCompleted || pur.Status == domain.StatusCancelled {
			continue
		}
		dateStr := pur.DueDate
		if dateStr == "" {
			dateStr = pur.Date
		}
		daysAway, ok := parseDaysAway(dateStr)
		if !ok || daysAway > days {
			continue
		}
		desc := pur.ProviderName
		if desc == "" {
			desc = pur.Category
		}
		entries = append(entries, entry{
			alert: domain.ProjectionAlert{
				ID:          pur.ID,
				Icon:        "AlertCircle",
				Title:       pur.Detail,
				Description: desc,
				DueDate:     dateStr,
				Amount:      pur.Balance,
				Color:       "brand-danger",
			},
			daysAway: daysAway,
			amount:   pur.Balance,
		})
	}

	for _, t := range all {
		if !t.IsProjection || t.Status == domain.StatusCancelled {
			continue
		}
		daysAway, ok := parseDaysAway(t.Date)
		if !ok || daysAway > days {
			continue
		}
		color, icon := "brand-success", "FileCheck"
		if t.Type == domain.TypeEgreso {
			color, icon = "brand-danger", "AlertCircle"
		}
		entries = append(entries, entry{
			alert: domain.ProjectionAlert{
				ID:          t.ID,
				Icon:        icon,
				Title:       t.Description,
				Description: t.Category,
				DueDate:     t.Date,
				Amount:      t.Amount,
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
	actor, initial := actorFrom(r)
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: actor, Initial: initial, Action: "Creó proyección",
		Module: "Proyecciones", Color: "bg-purple-500",
	})
	jsonCreated(w, t)
}

func (h *ProjectionsHandler) Simulate(w http.ResponseWriter, r *http.Request) {
	var req domain.SimulateRequest
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	base, err := h.store.GetCurrentBalance()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	invoices, err := h.store.GetPendingInvoices()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	purchases, err := h.store.GetPendingPurchases()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	projections, err := h.store.GetPendingProjections(time.Now().AddDate(1, 0, 0))
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	for _, inv := range invoices {
		base += inv.Balance
	}
	for _, pur := range purchases {
		base -= pur.Balance
	}
	for _, t := range projections {
		if t.Type == domain.TypeIngreso {
			base += t.Amount
		} else {
			base -= t.Amount
		}
	}

	impact := base * (req.SalesGrowth / 100)
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
