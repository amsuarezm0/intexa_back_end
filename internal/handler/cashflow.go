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

	// ── Last 7 days (cash received/paid only) ────────────────────────────────
	days := make([]domain.CashFlowDay, 7)
	for i := 6; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		dy, dm, dd := day.Date()
		var ing, egr float64
		for _, t := range all {
			if t.IsProjection {
				continue
			}
			r := receivedAmount(t)
			if r == 0 {
				continue
			}
			ty, tm, td := txDate(t)
			if ty == dy && tm == dm && td == dd {
				if t.Type == domain.TypeIngreso {
					ing += r
				} else {
					egr += r
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

	// ── Projected balance = cash received to date ± expected flows within 30 days ──
	// currentBalance already uses cash-basis (receivedAmount), so Pendiente and
	// remaining Parcial balances are not yet counted — we add them here.
	balance := currentBalance(all)
	horizon := now.AddDate(0, 0, 30)
	var pendingInc, pendingExp float64
	for _, t := range all {
		if t.IsProjection {
			continue
		}
		owed := pendingAmount(t)
		if owed == 0 {
			continue
		}
		ty, tm, td := txDate(t)
		tDate := time.Date(ty, tm, td, 0, 0, 0, 0, now.Location())
		if tDate.After(horizon) {
			continue
		}
		if t.Type == domain.TypeIngreso {
			pendingInc += owed
		} else {
			pendingExp += owed
		}
	}
	projected := balance + pendingInc - pendingExp

	// ── Month-over-month net change ───────────────────────────────────────
	cy, cm, _ := now.Date()
	prevTime := now.AddDate(0, -1, 0)
	py, pm, _ := prevTime.Date()
	thisInc, thisExp := monthlyTotals(all, cy, cm)
	prevInc, prevExp := monthlyTotals(all, py, pm)
	thisNet := thisInc - thisExp
	prevNet := prevInc - prevExp
	projectedChange := 0.0
	if prevNet != 0 {
		raw := (thisNet - prevNet) / math.Abs(prevNet) * 100
		projectedChange = math.Round(raw*10) / 10
		if projectedChange > 999 {
			projectedChange = 999
		} else if projectedChange < -999 {
			projectedChange = -999
		}
	}

	// ── Alerts: pending and partial transactions ──────────────────────────────
	alerts := []domain.Alert{}
	for _, t := range all {
		if t.IsProjection {
			continue
		}
		owed := pendingAmount(t)
		if owed == 0 {
			continue
		}
		kind := "success"
		title := "Cobro Pendiente"
		if t.Status == domain.StatusPartial {
			title = "Cobro Parcial"
		}
		if t.Type == domain.TypeEgreso {
			kind = "danger"
			title = "Pago Pendiente"
			if t.Status == domain.StatusPartial {
				title = "Pago Parcial"
			}
		}
		alerts = append(alerts, domain.Alert{
			ID:          t.ID,
			Type:        kind,
			Title:       title,
			Description: t.Description,
			Amount:      owed,
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
