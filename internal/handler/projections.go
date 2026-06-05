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
	now := time.Now().Truncate(24 * time.Hour)
	balance := currentBalance(all)

	// Index all transactions for O(1) parent lookup.
	txByID := make(map[string]*domain.Transaction, len(all))
	for _, t := range all {
		txByID[t.ID] = t
	}

	// Group payment-term projections by parent ID, sorted by due date (earliest first).
	type ptEntry struct {
		tx      *domain.Transaction
		dueDate time.Time
	}
	parentToTerms := map[string][]ptEntry{}
	for _, t := range all {
		if t.ParentID == "" || !t.IsProjection {
			continue
		}
		d, err := time.Parse("2006-01-02", t.Date)
		if err != nil {
			continue
		}
		parentToTerms[t.ParentID] = append(parentToTerms[t.ParentID], ptEntry{t, d})
	}
	for pid := range parentToTerms {
		terms := parentToTerms[pid]
		sort.Slice(terms, func(i, j int) bool { return terms[i].dueDate.Before(terms[j].dueDate) })
		parentToTerms[pid] = terms
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

	// Single-payment transactions: pending or partial, no payment-term children.
	for _, t := range all {
		if t.Status == domain.StatusCompleted || t.Status == domain.StatusCancelled {
			continue
		}
		if t.ParentID != "" && t.IsProjection {
			continue // handled via parent below
		}
		if _, hasTerms := parentToTerms[t.ID]; hasTerms {
			continue // handled via payment terms below
		}
		daysAway, ok := parseDaysAway(t.Date)
		if !ok || daysAway > days {
			continue
		}
		// Use remaining balance for partial invoices; full amount otherwise.
		amount := t.Amount
		if t.Balance > 0 {
			amount = t.Balance
		}
		addFlow(t.Type, amount, daysAway)
	}

	// Multi-payment transactions: distribute remaining balance across installments (FIFO).
	for parentID, terms := range parentToTerms {
		parent, ok := txByID[parentID]
		if !ok {
			continue
		}
		if parent.Status == domain.StatusCompleted || parent.Status == domain.StatusCancelled {
			continue
		}
		alreadyPaid := parent.Amount - parent.Balance
		for _, pt := range terms {
			if alreadyPaid >= pt.tx.Amount {
				alreadyPaid -= pt.tx.Amount // this installment is covered by past payments
				continue
			}
			effective := pt.tx.Amount - alreadyPaid
			alreadyPaid = 0
			daysAway, ok := parseDaysAway(pt.tx.Date)
			if !ok || daysAway > days {
				continue
			}
			addFlow(parent.Type, effective, daysAway)
		}
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

	// Alerts: show individual installments for multi-payment invoices, parent for single-payment.
	type entry struct {
		alert    domain.ProjectionAlert
		daysAway int
		amount   float64
	}
	var entries []entry

	addAlert := func(t *domain.Transaction, amount float64, daysAway int) {
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
				DueDate:     t.Date, // ISO YYYY-MM-DD
				Amount:      amount,
				Color:       color,
			},
			daysAway: daysAway,
			amount:   amount,
		})
	}

	for _, t := range all {
		if t.Status == domain.StatusCompleted || t.Status == domain.StatusCancelled {
			continue
		}
		if t.ParentID != "" && t.IsProjection {
			continue
		}
		if _, hasTerms := parentToTerms[t.ID]; hasTerms {
			continue
		}
		daysAway, ok := parseDaysAway(t.Date)
		if !ok || daysAway > days {
			continue
		}
		amount := t.Amount
		if t.Balance > 0 {
			amount = t.Balance
		}
		addAlert(t, amount, daysAway)
	}

	for parentID, terms := range parentToTerms {
		parent, ok := txByID[parentID]
		if !ok {
			continue
		}
		if parent.Status == domain.StatusCompleted || parent.Status == domain.StatusCancelled {
			continue
		}
		alreadyPaid := parent.Amount - parent.Balance
		for _, pt := range terms {
			if alreadyPaid >= pt.tx.Amount {
				alreadyPaid -= pt.tx.Amount
				continue
			}
			effective := pt.tx.Amount - alreadyPaid
			alreadyPaid = 0
			daysAway, ok := parseDaysAway(pt.tx.Date)
			if !ok || daysAway > days {
				continue
			}
			addAlert(pt.tx, effective, daysAway)
		}
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
	// Include pending and partial flows in base
	for _, t := range all {
		if t.IsProjection || (t.Status != domain.StatusPending && t.Status != domain.StatusPartial) {
			continue
		}
		amount := t.Amount
		if t.Balance > 0 {
			amount = t.Balance
		}
		if t.Type == domain.TypeIngreso {
			base += amount
		} else {
			base -= amount
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
