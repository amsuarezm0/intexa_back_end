package handler

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
	siigopkg "github.com/intexa/arca-api/internal/siigo"
)

const (
	siigoPageSize       = 100
	incrementalDays     = 90   // rolling window for daily sync
	schedulerHour       = 6    // daily sync fires at 06:00 local time
	reconcileDay        = 1    // reconcile fires on the 1st of each month
	bootstrapFallback   = 365  // days back if no Siigo records exist in reconcile
)

type SiigoHandler struct {
	store  repository.Store
	mu     sync.Mutex
	client *siigopkg.Client
}

func NewSiigoHandler(store repository.Store) *SiigoHandler {
	return &SiigoHandler{store: store}
}

// AutoConnect is called on startup with credentials from env vars.
func (h *SiigoHandler) AutoConnect(userName, accessKey, partnerID string) error {
	client := siigopkg.NewClient(userName, accessKey, partnerID)
	if err := client.Connect(); err != nil {
		return err
	}
	h.mu.Lock()
	h.client = client
	h.mu.Unlock()
	cfg := domain.SiigoConfig{
		UserName:  userName,
		AccessKey: accessKey,
		PartnerID: partnerID,
		Connected: true,
		TokenExp:  client.TokenExpiry(),
	}
	return h.store.SetSiigoConfig(cfg)
}

// StartScheduler launches the background sync goroutine. Call once on startup.
func (h *SiigoHandler) StartScheduler() {
	go h.runScheduler()
}

func (h *SiigoHandler) runScheduler() {
	for {
		now := time.Now()
		next := nextFireTime(now)
		log.Printf("siigo scheduler: next run at %s", next.Format("2006-01-02 15:04"))
		time.Sleep(time.Until(next))

		mode := domain.SyncModeIncremental
		if time.Now().Day() == reconcileDay {
			mode = domain.SyncModeReconcile
		}

		client, err := h.ensureClient()
		if err != nil {
			log.Printf("siigo scheduler: cannot get client: %v", err)
			continue
		}

		dateStart, dateEnd, err := h.resolveDates(mode, "")
		if err != nil {
			log.Printf("siigo scheduler: cannot resolve dates: %v", err)
			continue
		}

		result, err := h.runSync(client, mode, dateStart, dateEnd)
		if err != nil {
			log.Printf("siigo scheduler: sync failed: %v", err)
			continue
		}
		log.Printf("siigo scheduler: %s done — +%d FV, +%d FC, ~%d updated",
			mode, result.InvoicesImported, result.PurchasesImported, result.Updated)
	}
}

// nextFireTime returns the next 06:00 wall-clock time after now.
func nextFireTime(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), schedulerHour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

// POST /api/v1/siigo/connect
func (h *SiigoHandler) Connect(w http.ResponseWriter, r *http.Request) {
	var req domain.SiigoConnectRequest
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.UserName == "" || req.AccessKey == "" {
		jsonError(w, "userName and accessKey are required", http.StatusBadRequest)
		return
	}

	client := siigopkg.NewClient(req.UserName, req.AccessKey, req.PartnerID)
	if err := client.Connect(); err != nil {
		jsonError(w, fmt.Sprintf("siigo authentication failed: %v", err), http.StatusBadGateway)
		return
	}

	h.mu.Lock()
	h.client = client
	h.mu.Unlock()

	cfg := domain.SiigoConfig{
		UserName:  req.UserName,
		AccessKey: req.AccessKey,
		PartnerID: req.PartnerID,
		Connected: true,
		TokenExp:  client.TokenExpiry(),
	}
	if err := h.store.SetSiigoConfig(cfg); err != nil {
		jsonError(w, "failed to save config", http.StatusInternalServerError)
		return
	}
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: "Sistema", Initial: "SI", Action: "Conexión Siigo",
		Module: "Integración", Color: "bg-green-500",
	})
	jsonOK(w, cfg)
}

// GET /api/v1/siigo/status
func (h *SiigoHandler) Status(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.store.GetSiigoConfig()
	if err != nil {
		jsonError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if cfg == nil {
		jsonOK(w, map[string]any{"connected": false})
		return
	}
	jsonOK(w, cfg)
}

// POST /api/v1/siigo/sync
func (h *SiigoHandler) Sync(w http.ResponseWriter, r *http.Request) {
	var req domain.SiigoSyncRequest
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Mode == "" {
		req.Mode = domain.SyncModeIncremental
	}
	if req.Mode == domain.SyncModeBootstrap && req.DateStart == "" {
		jsonError(w, "dateStart is required for bootstrap mode", http.StatusBadRequest)
		return
	}

	client, err := h.ensureClient()
	if err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}

	dateStart, dateEnd, err := h.resolveDates(req.Mode, req.DateStart)
	if err != nil {
		jsonError(w, fmt.Sprintf("could not resolve sync window: %v", err), http.StatusInternalServerError)
		return
	}
	if req.DateEnd != "" {
		dateEnd = req.DateEnd
	}

	result, err := h.runSync(client, req.Mode, dateStart, dateEnd)
	if err != nil {
		jsonError(w, fmt.Sprintf("sync failed: %v", err), http.StatusBadGateway)
		return
	}
	jsonOK(w, result)
}

// ── core sync logic ───────────────────────────────────────────────────────────

func (h *SiigoHandler) ensureClient() (*siigopkg.Client, error) {
	h.mu.Lock()
	client := h.client
	h.mu.Unlock()

	if client != nil && client.IsConnected() {
		return client, nil
	}

	cfg, err := h.store.GetSiigoConfig()
	if err != nil || cfg == nil || cfg.AccessKey == "" {
		return nil, fmt.Errorf("not connected to Siigo — call /siigo/connect first")
	}
	c := siigopkg.NewClient(cfg.UserName, cfg.AccessKey, cfg.PartnerID)
	if err := c.Connect(); err != nil {
		return nil, fmt.Errorf("siigo re-authentication failed: %w", err)
	}
	h.mu.Lock()
	h.client = c
	h.mu.Unlock()
	return c, nil
}

// resolveDates returns the (dateStart, dateEnd) string pair for a given mode.
func (h *SiigoHandler) resolveDates(mode domain.SiigoSyncMode, requestedStart string) (string, string, error) {
	today := time.Now().Format("2006-01-02")
	switch mode {
	case domain.SyncModeBootstrap:
		return requestedStart, today, nil

	case domain.SyncModeReconcile:
		earliest, err := h.store.GetEarliestSiigoDate()
		if err != nil {
			return "", "", err
		}
		if earliest == "" {
			// No Siigo records yet — fall back to bootstrapFallback days
			earliest = time.Now().AddDate(0, 0, -bootstrapFallback).Format("2006-01-02")
		}
		return earliest, today, nil

	default: // incremental
		return time.Now().AddDate(0, 0, -incrementalDays).Format("2006-01-02"), today, nil
	}
}

// runSync fetches all pages for the given window and upserts into the store.
func (h *SiigoHandler) runSync(client *siigopkg.Client, mode domain.SiigoSyncMode, dateStart, dateEnd string) (*domain.SiigoSyncResult, error) {
	result := &domain.SiigoSyncResult{Mode: mode, DateStart: dateStart, DateEnd: dateEnd}

	if err := h.syncInvoices(client, dateStart, dateEnd, result); err != nil {
		return nil, err
	}
	if err := h.syncPurchases(client, dateStart, dateEnd, result); err != nil {
		return nil, err
	}

	h.store.UpdateSiigoLastSync(time.Now()) //nolint
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: "Sistema", Initial: "SI",
		Action: fmt.Sprintf("Sync Siigo [%s] (+%d FV, +%d FC, ~%d actualizados)",
			mode, result.InvoicesImported, result.PurchasesImported, result.Updated),
		Module: "Integración", Color: "bg-green-500",
	})
	return result, nil
}

func (h *SiigoHandler) syncInvoices(client *siigopkg.Client, dateStart, dateEnd string, result *domain.SiigoSyncResult) error {
	for page := 1; ; page++ {
		resp, err := client.GetInvoices(dateStart, dateEnd, page, siigoPageSize)
		if err != nil {
			return fmt.Errorf("fetching invoices page %d: %w", page, err)
		}
		for _, inv := range resp.Results {
			desc := fmt.Sprintf("Factura %s-%d", inv.Prefix, inv.Number)
			if inv.Customer.CommercialName != "" {
				desc += " — " + inv.Customer.CommercialName
			} else if inv.Customer.Name != "" {
				desc += " — " + inv.Customer.Name
			}
			t := &domain.Transaction{
				Date: inv.Date, Description: desc,
				Category: "Operacional - Ventas", Type: domain.TypeIngreso,
				Amount: inv.Total, Status: invoiceStatus(inv.Balance),
				Reference:  fmt.Sprintf("%s-%d", inv.Prefix, inv.Number),
				Source:     domain.SourceSIIGO,
				ExternalID: fmt.Sprintf("siigo-inv-%s", inv.ID),
			}
			inserted, err := h.store.ImportTransaction(t)
			if err != nil {
				return fmt.Errorf("saving invoice %s: %w", inv.ID, err)
			}
			if inserted {
				result.InvoicesImported++
			} else {
				result.Updated++
			}
		}
		if page*siigoPageSize >= resp.Pagination.TotalResults {
			break
		}
	}
	return nil
}

func (h *SiigoHandler) syncPurchases(client *siigopkg.Client, dateStart, dateEnd string, result *domain.SiigoSyncResult) error {
	for page := 1; ; page++ {
		resp, err := client.GetPurchases(dateStart, dateEnd, page, siigoPageSize)
		if err != nil {
			return fmt.Errorf("fetching purchases page %d: %w", page, err)
		}
		for _, pur := range resp.Results {
			desc := fmt.Sprintf("Factura Proveedor %s-%d", pur.Prefix, pur.Number)
			if pur.Provider.CommercialName != "" {
				desc += " — " + pur.Provider.CommercialName
			} else if pur.Provider.Name != "" {
				desc += " — " + pur.Provider.Name
			}
			t := &domain.Transaction{
				Date: pur.Date, Description: desc,
				Category: "Gastos Operativos", Type: domain.TypeEgreso,
				Amount: pur.Total, Status: invoiceStatus(pur.Balance),
				Reference:  fmt.Sprintf("%s-%d", pur.Prefix, pur.Number),
				Source:     domain.SourceSIIGO,
				ExternalID: fmt.Sprintf("siigo-pur-%s", pur.ID),
			}
			inserted, err := h.store.ImportTransaction(t)
			if err != nil {
				return fmt.Errorf("saving purchase %s: %w", pur.ID, err)
			}
			if inserted {
				result.PurchasesImported++
			} else {
				result.Updated++
			}
		}
		if page*siigoPageSize >= resp.Pagination.TotalResults {
			break
		}
	}
	return nil
}

func invoiceStatus(balance float64) domain.TransactionStatus {
	if balance == 0 {
		return domain.StatusCompleted
	}
	return domain.StatusPending
}
