package handler

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
)

type CustomersHandler struct {
	store repository.Store
}

func NewCustomersHandler(store repository.Store) *CustomersHandler {
	return &CustomersHandler{store: store}
}

// List returns the synced third parties, filtered and paginated.
//
// GET /api/v1/customers
//
//	?search=      name, commercial name, identification or email (substring)
//	?type=        Cliente | Proveedor | Otro   (default: Cliente)
//	?active=      true | false | all          (default: true)
//	?withBalance= true — only those still owing on an invoice
//	?sort=        name | pending | invoiced | recent
func (h *CustomersHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := strings.ToLower(strings.TrimSpace(q.Get("search")))
	typeFilter := q.Get("type")
	activeParam := q.Get("active")
	withBalance := q.Get("withBalance") == "true"
	sortBy := q.Get("sort")
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	// "all" is the explicit opt-out; anything else narrows to one type, and an
	// unset filter shows clients — this is the Clientes module, after all.
	if typeFilter == "" {
		typeFilter = string(domain.CustomerTypeCustomer)
	}

	all, err := h.listWithAggregates()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	filtered := make([]domain.Customer, 0, len(all))
	for _, c := range all {
		if typeFilter != "all" && string(c.Type) != typeFilter {
			continue
		}
		// Inactive rows are third parties the last clean sync no longer found
		// in Siigo; they stay out of the way unless asked for.
		switch activeParam {
		case "false":
			if c.Active {
				continue
			}
		case "all":
			// no filter
		default: // "" or "true"
			if !c.Active {
				continue
			}
		}
		if withBalance && c.PendingBalance <= 0 {
			continue
		}
		if search != "" && !customerMatches(c, search) {
			continue
		}
		filtered = append(filtered, *c)
	}

	sortCustomers(filtered, sortBy)

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

	jsonOK(w, domain.CustomerListResponse{
		Data:       filtered[start:end],
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
	})
}

// Summary is the header strip of the Clientes view: how many third parties are
// on file and how much of the portfolio they account for.
//
// GET /api/v1/customers/summary
func (h *CustomersHandler) Summary(w http.ResponseWriter, r *http.Request) {
	all, err := h.listWithAggregates()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var (
		customers, suppliers, others, withBalance int
		pending, invoiced                         float64
	)
	for _, c := range all {
		if !c.Active {
			continue
		}
		switch c.Type {
		case domain.CustomerTypeCustomer:
			customers++
		case domain.CustomerTypeSupplier:
			suppliers++
		default:
			others++
		}
		if c.Type != domain.CustomerTypeCustomer {
			continue
		}
		invoiced += c.TotalInvoiced
		if c.PendingBalance > 0 {
			withBalance++
			pending += c.PendingBalance
		}
	}

	jsonOK(w, map[string]any{
		"customers":           customers,
		"suppliers":           suppliers,
		"others":              others,
		"customersWithDebt":   withBalance,
		"totalPendingBalance": pending,
		"totalInvoiced":       invoiced,
	})
}

// Get returns one third party plus the invoices issued to its identification.
//
// GET /api/v1/customers/{id}
func (h *CustomersHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, ok, err := h.store.GetCustomerByID(id)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		jsonError(w, "cliente no encontrado", http.StatusNotFound)
		return
	}

	invoices, err := h.store.GetInvoicesByCustomer(c.Identification)
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	for _, inv := range invoices {
		c.InvoiceCount++
		c.TotalInvoiced += inv.Total
		if inv.Status == domain.StatusPending || inv.Status == domain.StatusPartial {
			c.PendingBalance += inv.Balance
		}
		if inv.Date > c.LastInvoiceDate {
			c.LastInvoiceDate = inv.Date
		}
	}

	attachDueDatesToInvoices(invoices)
	jsonOK(w, map[string]any{
		"customer": c,
		"invoices": invoices,
	})
}

// listWithAggregates loads every customer with its invoice roll-up applied.
func (h *CustomersHandler) listWithAggregates() ([]*domain.Customer, error) {
	all, err := h.store.GetAllCustomers()
	if err != nil {
		return nil, err
	}
	aggregates, err := h.store.GetCustomerAggregates()
	if err != nil {
		return nil, err
	}
	for _, c := range all {
		a, ok := aggregates[c.Identification]
		if !ok {
			continue
		}
		c.InvoiceCount = a.InvoiceCount
		c.TotalInvoiced = a.TotalInvoiced
		c.PendingBalance = a.PendingBalance
		c.LastInvoiceDate = a.LastInvoiceDate
	}
	return all, nil
}

func customerMatches(c *domain.Customer, search string) bool {
	for _, field := range []string{c.Name, c.CommercialName, c.Identification, c.Email, c.Phone} {
		if strings.Contains(strings.ToLower(field), search) {
			return true
		}
	}
	return false
}

func sortCustomers(list []domain.Customer, sortBy string) {
	switch sortBy {
	case "pending":
		sort.SliceStable(list, func(i, j int) bool { return list[i].PendingBalance > list[j].PendingBalance })
	case "invoiced":
		sort.SliceStable(list, func(i, j int) bool { return list[i].TotalInvoiced > list[j].TotalInvoiced })
	case "recent":
		sort.SliceStable(list, func(i, j int) bool { return list[i].LastInvoiceDate > list[j].LastInvoiceDate })
	default:
		sort.SliceStable(list, func(i, j int) bool {
			return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
		})
	}
}
