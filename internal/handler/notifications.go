package handler

import (
	"net/http"
	"sort"
	"time"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
)

type NotificationsHandler struct {
	store repository.Store
}

func NewNotificationsHandler(store repository.Store) *NotificationsHandler {
	return &NotificationsHandler{store: store}
}

// GET /api/v1/notifications
func (h *NotificationsHandler) GetNotifications(w http.ResponseWriter, r *http.Request) {
	all, err := h.store.GetAllTransactions()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	var gastos, ingresos []domain.NotificationItem

	for _, t := range all {
		if t.Status != domain.StatusPending || t.IsProjection {
			continue
		}
		d, err := time.Parse("2006-01-02", t.Date)
		if err != nil {
			d = t.CreatedAt
		}
		daysOverdue := int(now.Sub(d).Hours() / 24)

		urgency := "upcoming"
		if daysOverdue > 0 {
			urgency = "overdue"
		} else if daysOverdue >= -7 {
			urgency = "due-soon"
		}

		item := domain.NotificationItem{
			ID:          t.ID,
			Title:       t.Description,
			Category:    t.Category,
			Amount:      t.Amount,
			Date:        t.Date,
			DaysOverdue: daysOverdue,
			Urgency:     urgency,
		}

		if t.Type == domain.TypeEgreso {
			gastos = append(gastos, item)
		} else {
			ingresos = append(ingresos, item)
		}
	}

	// Sort: overdue first, then by amount desc within each urgency group
	sortItems := func(items []domain.NotificationItem) {
		sort.Slice(items, func(i, j int) bool {
			ui, uj := urgencyRank(items[i].Urgency), urgencyRank(items[j].Urgency)
			if ui != uj {
				return ui > uj
			}
			return items[i].Amount > items[j].Amount
		})
	}
	sortItems(gastos)
	sortItems(ingresos)

	jsonOK(w, domain.NotificationSummary{
		Count:    len(gastos) + len(ingresos),
		Gastos:   nilSafe(gastos),
		Ingresos: nilSafe(ingresos),
	})
}

func urgencyRank(u string) int {
	switch u {
	case "overdue":
		return 3
	case "due-soon":
		return 2
	default:
		return 1
	}
}

func nilSafe(items []domain.NotificationItem) []domain.NotificationItem {
	if items == nil {
		return []domain.NotificationItem{}
	}
	return items
}
