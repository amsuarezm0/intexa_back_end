package handler

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
)

type TransactionsHandler struct {
	store repository.Store
}

func NewTransactionsHandler(store repository.Store) *TransactionsHandler {
	return &TransactionsHandler{store: store}
}

func (h *TransactionsHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := strings.ToLower(q.Get("search"))
	typeFilter := q.Get("type")
	statusFilter := q.Get("status")
	dateFrom := q.Get("dateFrom")   // YYYY-MM-DD inclusive
	dateTo := q.Get("dateTo")       // YYYY-MM-DD inclusive
	sourceFilter := q.Get("source") // "Siigo", "Manual", or ""
	isProjectionParam := q.Get("isProjection") // "true", "false", or "" (no filter)
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	all, err := h.store.GetAllTransactions()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Date != all[j].Date {
			return all[i].Date > all[j].Date
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})

	filtered := make([]domain.Transaction, 0)
	for _, t := range all {
		if search != "" && !strings.Contains(strings.ToLower(t.Description), search) &&
			!strings.Contains(strings.ToLower(t.Category), search) {
			continue
		}
		if typeFilter != "" && string(t.Type) != typeFilter {
			continue
		}
		if statusFilter != "" && string(t.Status) != statusFilter {
			continue
		}
		if dateFrom != "" && t.Date < dateFrom {
			continue
		}
		if dateTo != "" && t.Date > dateTo {
			continue
		}
		if sourceFilter != "" && string(t.Source) != sourceFilter {
			continue
		}
		if isProjectionParam == "true" && !t.IsProjection {
			continue
		}
		if isProjectionParam == "false" && t.IsProjection {
			continue
		}
		filtered = append(filtered, *t)
	}

	total := len(filtered)
	totalPages := (total + limit - 1) / limit
	start := (page - 1) * limit
	end := start + limit
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	jsonOK(w, domain.TransactionListResponse{
		Data:       filtered[start:end],
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
	})
}

func (h *TransactionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, ok, err := h.store.GetTransactionByID(id)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		jsonError(w, "transaction not found", http.StatusNotFound)
		return
	}
	jsonOK(w, t)
}

func (h *TransactionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var t domain.Transaction
	if err := decode(r, &t); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.store.CreateTransaction(&t); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonCreated(w, t)
}

func (h *TransactionsHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, ok, err := h.store.GetTransactionByID(id)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		jsonError(w, "transaction not found", http.StatusNotFound)
		return
	}
	if existing.Source == domain.SourceSIIGO {
		jsonError(w, "las transacciones de Siigo no pueden ser modificadas", http.StatusForbidden)
		return
	}
	var t domain.Transaction
	if err := decode(r, &t); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	t.ID = id
	if _, err := h.store.UpdateTransaction(&t); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, t)
}

func (h *TransactionsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, ok, err := h.store.GetTransactionByID(id)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		jsonError(w, "transaction not found", http.StatusNotFound)
		return
	}
	if existing.Source == domain.SourceSIIGO {
		jsonError(w, "las transacciones de Siigo no pueden ser eliminadas", http.StatusForbidden)
		return
	}
	if _, err := h.store.DeleteTransaction(id); err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"message": "deleted"})
}

func (h *TransactionsHandler) Summary(w http.ResponseWriter, r *http.Request) {
	all, err := h.store.GetAllTransactions()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	now := time.Now()
	curY, curM := now.Year(), now.Month()
	var totalBalance, totalIncome, totalExpense, monthlyIncome, monthlyExpense float64
	for _, t := range all {
		if t.IsProjection || t.Status == domain.StatusCancelled {
			continue
		}
		r := receivedAmount(t)
		if r == 0 {
			continue
		}
		if t.Type == domain.TypeIngreso {
			totalBalance += r
			totalIncome += r
		} else {
			totalBalance -= r
			totalExpense += r
		}
		y, m, _ := txDate(t)
		if y == curY && m == curM {
			if t.Type == domain.TypeIngreso {
				monthlyIncome += r
			} else {
				monthlyExpense += r
			}
		}
	}
	jsonOK(w, domain.TransactionSummary{
		TotalBalance:   totalBalance,
		TotalIncome:    totalIncome,
		TotalExpense:   totalExpense,
		MonthlyIncome:  monthlyIncome,
		MonthlyExpense: monthlyExpense,
	})
}
