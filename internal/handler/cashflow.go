package handler

import (
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
)

type CashFlowHandler struct {
	store repository.Store
}

func NewCashFlowHandler(store repository.Store) *CashFlowHandler {
	return &CashFlowHandler{store: store}
}

var weekdayES = [7]string{"DOM", "LUN", "MAR", "MIÉ", "JUE", "VIE", "SÁB"}

func (h *CashFlowHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	all, err := h.store.GetAllTransactions()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	now := time.Now()

	// ── Last 7 days ──────────────────────────────────────────────────────────
	days := make([]domain.CashFlowDay, 7)
	for i := 6; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		dy, dm, dd := day.Date()
		var ing, egr float64
		for _, t := range all {
			if t.IsProjection {
				continue
			}
			ty, tm, td := t.CreatedAt.Date()
			if ty == dy && tm == dm && td == dd {
				if t.Type == domain.TypeIngreso {
					ing += t.Amount
				} else {
					egr += t.Amount
				}
			}
		}
		days[6-i] = domain.CashFlowDay{
			Label:    weekdayES[day.Weekday()],
			Date:     dd,
			Ingresos: ing,
			Egresos:  egr,
		}
	}

	// ── Projected balance = current balance ± pending transactions ────────
	balance := currentBalance(all)
	var pendingInc, pendingExp float64
	for _, t := range all {
		if t.Status != domain.StatusPending || t.IsProjection {
			continue
		}
		if t.Type == domain.TypeIngreso {
			pendingInc += t.Amount
		} else {
			pendingExp += t.Amount
		}
	}
	projected := balance + pendingInc - pendingExp

	// ── Week-over-week change ─────────────────────────────────────────────
	var thisWeekNet, prevWeekNet float64
	for i := 6; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		dy, dm, dd := day.Date()
		for _, t := range all {
			if t.IsProjection {
				continue
			}
			ty, tm, td := t.CreatedAt.Date()
			if ty == dy && tm == dm && td == dd {
				if t.Type == domain.TypeIngreso {
					thisWeekNet += t.Amount
				} else {
					thisWeekNet -= t.Amount
				}
			}
		}
	}
	for i := 13; i >= 7; i-- {
		day := now.AddDate(0, 0, -i)
		dy, dm, dd := day.Date()
		for _, t := range all {
			if t.IsProjection {
				continue
			}
			ty, tm, td := t.CreatedAt.Date()
			if ty == dy && tm == dm && td == dd {
				if t.Type == domain.TypeIngreso {
					prevWeekNet += t.Amount
				} else {
					prevWeekNet -= t.Amount
				}
			}
		}
	}
	projectedChange := 0.0
	if prevWeekNet != 0 {
		projectedChange = (thisWeekNet - prevWeekNet) / math.Abs(prevWeekNet) * 100
	}

	// ── Alerts: pending transactions ─────────────────────────────────────
	alerts := []domain.Alert{}
	for _, t := range all {
		if t.Status != domain.StatusPending || t.IsProjection {
			continue
		}
		kind := "success"
		title := "Cobro Pendiente"
		if t.Type == domain.TypeEgreso {
			kind = "danger"
			title = "Pago Pendiente"
		}
		alerts = append(alerts, domain.Alert{
			ID:          t.ID,
			Type:        kind,
			Title:       title,
			Description: t.Description,
			Amount:      t.Amount,
			DueDate:     t.Date,
		})
	}
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].Amount > alerts[j].Amount })
	if len(alerts) > 4 {
		alerts = alerts[:4]
	}

	jsonOK(w, domain.CashFlowSummary{
		Days:             days,
		ProjectedBalance: projected,
		ProjectedChange:  projectedChange,
		Alerts:           alerts,
	})
}
