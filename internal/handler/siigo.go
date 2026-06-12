package handler

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
	siigopkg "github.com/intexa/arca-api/internal/siigo"
)

const (
	siigoPageSize      = 100
	siigoRetryAttempts = 3
	siigoMaxConcurrent = 8
	incrementalDays    = 90  // rolling window for incremental (days back from today)
	schedulerHour      = 6   // daily sync fires at 06:00 local time
	reconcileDay       = 1   // reconcile fires on the 1st of each month
	bootstrapFallback  = 730 // days back when no records exist (2 years)
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
	client.StartAutoRefresh()
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

		result, err := h.runSync(client, mode, dateStart, dateEnd, "Sistema", "SI")
		if err != nil {
			slog.Error("siigo_sync_failed", "mode", mode, "error", err)
			continue
		}
		slog.Info("siigo_sync_done",
			"mode",                      mode,
			"invoices_imported",         result.InvoicesImported,
			"purchases_imported",        result.PurchasesImported,
			"vouchers_imported",         result.VouchersImported,
			"payment_receipts_imported", result.PaymentReceiptsImported,
			"updated",                   result.Updated,
			"date_start",                dateStart,
			"date_end",                  dateEnd,
		)
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
	client.StartAutoRefresh()

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
	actor, initial := actorFrom(r)
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: actor, Initial: initial, Action: "Conexión Siigo",
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
		jsonError(w, "Cuerpo de solicitud inválido", http.StatusBadRequest)
		return
	}
	if req.Mode == "" {
		req.Mode = domain.SyncModeIncremental
	}
	if req.Mode == domain.SyncModeBootstrap && req.DateStart == "" {
		jsonError(w, "dateStart es requerido para el modo bootstrap", http.StatusBadRequest)
		return
	}

	client, err := h.ensureClient()
	if err != nil {
		jsonError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	dateStart, dateEnd, err := h.resolveDates(req.Mode, req.DateStart)
	if err != nil {
		jsonError(w, fmt.Sprintf("No se pudo determinar el rango de fechas: %v", err), http.StatusInternalServerError)
		return
	}

	actor, initial := actorFrom(r)
	result, err := h.runSync(client, req.Mode, dateStart, dateEnd, actor, initial)
	if err != nil {
		jsonError(w, "Error al guardar datos de sincronización", http.StatusInternalServerError)
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
		return nil, fmt.Errorf("no hay conexión con Siigo — configure las credenciales primero")
	}
	c := siigopkg.NewClient(cfg.UserName, cfg.AccessKey, cfg.PartnerID)
	if err := c.Connect(); err != nil {
		return nil, fmt.Errorf("error de reautenticación con Siigo: %w", err)
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
		oldest, err := h.store.GetOldestPendingOrPartialDate()
		if err != nil {
			return "", "", err
		}
		if oldest == "" {
			oldest = time.Now().AddDate(0, 0, -incrementalDays).Format("2006-01-02")
		}
		slog.Info("siigo_reconcile_window", "date_start", oldest, "date_end", today)
		return oldest, today, nil

	default: // incremental
		return time.Now().AddDate(0, 0, -incrementalDays).Format("2006-01-02"), today, nil
	}
}

// withRetry calls fn up to attempts times, sleeping 1s, 2s, … between failures.
func withRetry(attempts int, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i < attempts-1 {
			time.Sleep(time.Duration(1<<uint(i)) * time.Second)
		}
	}
	return err
}

// friendlySiigoErr translates a Siigo API error into a short Spanish description.
func friendlySiigoErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "document_query_service"):
		return "servicio de Siigo no disponible temporalmente"
	case strings.Contains(s, "unhandled_error"):
		return "error interno en Siigo"
	case strings.Contains(s, "context deadline exceeded"), strings.Contains(s, "Client.Timeout"):
		return "tiempo de espera agotado"
	case strings.Contains(s, "error 503"):
		return "servicio no disponible (503)"
	case strings.Contains(s, "error 500"):
		return "error interno del servidor (500)"
	case strings.Contains(s, "error 401"), strings.Contains(s, "error 403"):
		return "error de autenticación"
	default:
		return "error de comunicación con Siigo"
	}
}

func siigoPageErr(docType string, page int, err error) string {
	return fmt.Sprintf("%s — página %d: %s", docType, page, friendlySiigoErr(err))
}

// runSync fetches all pages for the given window concurrently and upserts into the store.
// All four resource types run in parallel; within each type all pages run in parallel.
// A shared semaphore caps total concurrent Siigo requests to siigoMaxConcurrent.
// Siigo API failures are retried up to siigoRetryAttempts times; exhausted pages are recorded
// in result.Errors (Spanish) without aborting the overall sync. Only DB/infra errors propagate.
func (h *SiigoHandler) runSync(client *siigopkg.Client, mode domain.SiigoSyncMode, dateStart, dateEnd, actor, initial string) (*domain.SiigoSyncResult, error) {
	result := &domain.SiigoSyncResult{Mode: mode, DateStart: dateStart, DateEnd: dateEnd}
	var resultMu sync.Mutex
	sem := make(chan struct{}, siigoMaxConcurrent)

	syncs := []func() error{
		func() error { return h.syncInvoices(client, dateStart, dateEnd, result, &resultMu, sem) },
		func() error { return h.syncPurchases(client, dateStart, dateEnd, result, &resultMu, sem) },
		func() error { return h.syncVouchers(client, dateStart, dateEnd, result, &resultMu, sem) },
		func() error { return h.syncPaymentReceipts(client, dateStart, dateEnd, result, &resultMu, sem) },
	}

	dbErrs := make([]error, len(syncs))
	var wg sync.WaitGroup
	for i, fn := range syncs {
		wg.Add(1)
		go func(i int, fn func() error) {
			defer wg.Done()
			dbErrs[i] = fn()
		}(i, fn)
	}
	wg.Wait()

	// DB/infra errors are fatal — Siigo API page errors are already in result.Errors
	if err := errors.Join(dbErrs...); err != nil {
		return result, err
	}

	h.store.UpdateSiigoLastSync(time.Now())    //nolint
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: actor, Initial: initial,
		Action: fmt.Sprintf("Sync Siigo [%s] (+%d FV, +%d FC, +%d RC, +%d RP, ~%d actualizados)",
			mode, result.InvoicesImported, result.PurchasesImported,
			result.VouchersImported, result.PaymentReceiptsImported, result.Updated),
		Module: "Integración", Color: "bg-green-500",
	})
	if len(result.Errors) > 0 {
		slog.Warn("siigo_sync_partial",
			"mode",         mode,
			"actor",        actor,
			"page_errors",  len(result.Errors),
			"date_start",   dateStart,
			"date_end",     dateEnd,
		)
	} else {
		slog.Info("siigo_sync_complete",
			"mode",                      mode,
			"actor",                     actor,
			"invoices_imported",         result.InvoicesImported,
			"purchases_imported",        result.PurchasesImported,
			"vouchers_imported",         result.VouchersImported,
			"payment_receipts_imported", result.PaymentReceiptsImported,
			"updated",                   result.Updated,
			"date_start",                dateStart,
			"date_end",                  dateEnd,
		)
	}
	return result, nil
}

func (h *SiigoHandler) syncInvoices(client *siigopkg.Client, dateStart, dateEnd string, result *domain.SiigoSyncResult, mu *sync.Mutex, sem chan struct{}) error {
	var page1 *siigopkg.InvoiceListResponse
	if err := withRetry(siigoRetryAttempts, func() error {
		sem <- struct{}{}
		defer func() { <-sem }()
		var e error
		page1, e = client.GetInvoices(dateStart, dateEnd, 1, siigoPageSize)
		return e
	}); err != nil {
		mu.Lock()
		result.Errors = append(result.Errors, siigoPageErr("Facturas de Venta (FV)", 1, err))
		mu.Unlock()
		return nil
	}
	if err := h.saveInvoices(page1.Results, dateStart, dateEnd, result, mu); err != nil {
		return err
	}

	totalPages := (page1.Pagination.TotalResults + siigoPageSize - 1) / siigoPageSize
	if totalPages <= 1 {
		return nil
	}

	dbErrs := make([]error, totalPages-1)
	var wg sync.WaitGroup
	for page := 2; page <= totalPages; page++ {
		wg.Add(1)
		go func(page int) {
			defer wg.Done()
			var resp *siigopkg.InvoiceListResponse
			if err := withRetry(siigoRetryAttempts, func() error {
				sem <- struct{}{}
				defer func() { <-sem }()
				var e error
				resp, e = client.GetInvoices(dateStart, dateEnd, page, siigoPageSize)
				return e
			}); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, siigoPageErr("Facturas de Venta (FV)", page, err))
				mu.Unlock()
				return
			}
			dbErrs[page-2] = h.saveInvoices(resp.Results, dateStart, dateEnd, result, mu)
		}(page)
	}
	wg.Wait()
	return errors.Join(dbErrs...)
}

func (h *SiigoHandler) saveInvoices(invoices []siigopkg.Invoice, dateStart, dateEnd string, result *domain.SiigoSyncResult, mu *sync.Mutex) error {
	for _, inv := range invoices {
		if inv.Date < dateStart || inv.Date > dateEnd {
			continue
		}
		itemDescs := make([]string, 0, len(inv.Items))
		for _, it := range inv.Items {
			itemDescs = append(itemDescs, strings.TrimSpace(it.Description))
		}
		record := &domain.Invoice{
			ExternalID:             fmt.Sprintf("siigo-inv-%s", inv.ID),
			Source:                 string(domain.SourceSIIGO),
			IsProjection:           false,
			Reference:              inv.Name,
			Prefix:                 inv.Prefix,
			Number:                 inv.Number,
			Date:                   inv.Date,
			DueDate:                firstNonEmpty(inv.DueDate, firstPaymentDueDate(inv.Payments)),
			CustomerIdentification: inv.Customer.Identification,
			CustomerName:           firstNonEmpty(inv.Customer.Name, inv.Customer.CommercialName),
			Total:                  inv.Total,
			Balance:                inv.Balance,
			Status:                 invoiceStatus(inv.Balance, inv.Total),
			Category:               categorizeInvoice(itemDescs, inv.Customer.Name),
			Detail:                 inv.Name + ifNonEmpty(" · ", strings.Join(itemDescs, " | ")),
		}
		inserted, err := h.store.UpsertInvoice(record)
		if err != nil {
			return fmt.Errorf("error al guardar factura de venta %s: %w", inv.ID, err)
		}
		mu.Lock()
		if inserted {
			result.InvoicesImported++
		} else {
			result.Updated++
		}
		mu.Unlock()
	}
	return nil
}

func (h *SiigoHandler) syncPurchases(client *siigopkg.Client, dateStart, dateEnd string, result *domain.SiigoSyncResult, mu *sync.Mutex, sem chan struct{}) error {
	var page1 *siigopkg.PurchaseListResponse
	if err := withRetry(siigoRetryAttempts, func() error {
		sem <- struct{}{}
		defer func() { <-sem }()
		var e error
		page1, e = client.GetPurchases(dateStart, dateEnd, 1, siigoPageSize)
		return e
	}); err != nil {
		mu.Lock()
		result.Errors = append(result.Errors, siigoPageErr("Facturas de Compra (FC)", 1, err))
		mu.Unlock()
		return nil
	}
	if err := h.savePurchases(page1.Results, dateStart, dateEnd, result, mu); err != nil {
		return err
	}

	totalPages := (page1.Pagination.TotalResults + siigoPageSize - 1) / siigoPageSize
	if totalPages <= 1 {
		return nil
	}

	dbErrs := make([]error, totalPages-1)
	var wg sync.WaitGroup
	for page := 2; page <= totalPages; page++ {
		wg.Add(1)
		go func(page int) {
			defer wg.Done()
			var resp *siigopkg.PurchaseListResponse
			if err := withRetry(siigoRetryAttempts, func() error {
				sem <- struct{}{}
				defer func() { <-sem }()
				var e error
				resp, e = client.GetPurchases(dateStart, dateEnd, page, siigoPageSize)
				return e
			}); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, siigoPageErr("Facturas de Compra (FC)", page, err))
				mu.Unlock()
				return
			}
			dbErrs[page-2] = h.savePurchases(resp.Results, dateStart, dateEnd, result, mu)
		}(page)
	}
	wg.Wait()
	return errors.Join(dbErrs...)
}

func (h *SiigoHandler) savePurchases(purchases []siigopkg.Purchase, dateStart, dateEnd string, result *domain.SiigoSyncResult, mu *sync.Mutex) error {
	for _, pur := range purchases {
		if pur.Date < dateStart || pur.Date > dateEnd {
			continue
		}
		itemDescs := make([]string, 0, len(pur.Items))
		for _, it := range pur.Items {
			itemDescs = append(itemDescs, strings.TrimSpace(it.Description))
		}
		record := &domain.Purchase{
			ExternalID:             fmt.Sprintf("siigo-pur-%s", pur.ID),
			Source:                 string(domain.SourceSIIGO),
			IsProjection:           false,
			Reference:              pur.Name,
			Prefix:                 pur.Prefix,
			Number:                 pur.Number,
			Date:                   pur.Date,
			DueDate:                firstNonEmpty(pur.DueDate, firstPaymentDueDate(pur.Payments)),
			ProviderIdentification: pur.Provider.Identification,
			ProviderName:           firstNonEmpty(pur.Provider.Name, pur.Provider.CommercialName),
			Total:                  pur.Total,
			Balance:                pur.Balance,
			Status:                 invoiceStatus(pur.Balance, pur.Total),
			Category:               categorizePurchase(itemDescs, pur.Provider.Name),
			Detail:                 pur.Name + ifNonEmpty(" · ", strings.Join(itemDescs, " | ")),
		}
		inserted, err := h.store.UpsertPurchase(record)
		if err != nil {
			return fmt.Errorf("error al guardar factura de compra %s: %w", pur.ID, err)
		}
		mu.Lock()
		if inserted {
			result.PurchasesImported++
		} else {
			result.Updated++
		}
		mu.Unlock()
	}
	return nil
}

func (h *SiigoHandler) syncVouchers(client *siigopkg.Client, dateStart, dateEnd string, result *domain.SiigoSyncResult, mu *sync.Mutex, sem chan struct{}) error {
	var page1 *siigopkg.VoucherListResponse
	if err := withRetry(siigoRetryAttempts, func() error {
		sem <- struct{}{}
		defer func() { <-sem }()
		var e error
		page1, e = client.GetVouchers(dateStart, dateEnd, 1, siigoPageSize)
		return e
	}); err != nil {
		mu.Lock()
		result.Errors = append(result.Errors, siigoPageErr("Recibos de Cobro (RC)", 1, err))
		mu.Unlock()
		return nil
	}
	if err := h.saveVouchers(page1.Results, dateStart, dateEnd, result, mu); err != nil {
		return err
	}

	totalPages := (page1.Pagination.TotalResults + siigoPageSize - 1) / siigoPageSize
	if totalPages <= 1 {
		return nil
	}

	dbErrs := make([]error, totalPages-1)
	var wg sync.WaitGroup
	for page := 2; page <= totalPages; page++ {
		wg.Add(1)
		go func(page int) {
			defer wg.Done()
			var resp *siigopkg.VoucherListResponse
			if err := withRetry(siigoRetryAttempts, func() error {
				sem <- struct{}{}
				defer func() { <-sem }()
				var e error
				resp, e = client.GetVouchers(dateStart, dateEnd, page, siigoPageSize)
				return e
			}); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, siigoPageErr("Recibos de Cobro (RC)", page, err))
				mu.Unlock()
				return
			}
			dbErrs[page-2] = h.saveVouchers(resp.Results, dateStart, dateEnd, result, mu)
		}(page)
	}
	wg.Wait()
	return errors.Join(dbErrs...)
}

func (h *SiigoHandler) saveVouchers(vouchers []siigopkg.Voucher, dateStart, dateEnd string, result *domain.SiigoSyncResult, mu *sync.Mutex) error {
	for _, v := range vouchers {
		if v.Date < dateStart || v.Date > dateEnd {
			continue
		}
		itemDescs := make([]string, 0, len(v.Items))
		for _, it := range v.Items {
			if s := strings.TrimSpace(it.Description); s != "" {
				itemDescs = append(itemDescs, s)
			}
		}
		t := &domain.Transaction{
			Date:         v.Date,
			Description:  v.Name,
			Reference:    v.Name,
			Category:     categorizeInvoice(itemDescs, v.Customer.Name),
			Type:         domain.TypeIngreso,
			Amount:       voucherTotal(v.Total, v.Items, "Debit"),
			Status:       domain.StatusCompleted,
			Detail:       v.Name + ifNonEmpty(" · ", strings.Join(itemDescs, " | ")),
			Source:       domain.SourceSIIGO,
			ExternalID:   fmt.Sprintf("siigo-rc-%s", v.ID),
			IsProjection: false,
		}
		inserted, err := h.store.ImportTransaction(t)
		if err != nil {
			return fmt.Errorf("error al guardar recibo de cobro %s: %w", v.ID, err)
		}
		mu.Lock()
		if inserted {
			result.VouchersImported++
		} else {
			result.Updated++
		}
		mu.Unlock()
	}
	return nil
}

func (h *SiigoHandler) syncPaymentReceipts(client *siigopkg.Client, dateStart, dateEnd string, result *domain.SiigoSyncResult, mu *sync.Mutex, sem chan struct{}) error {
	var page1 *siigopkg.PaymentReceiptListResponse
	if err := withRetry(siigoRetryAttempts, func() error {
		sem <- struct{}{}
		defer func() { <-sem }()
		var e error
		page1, e = client.GetPaymentReceipts(dateStart, dateEnd, 1, siigoPageSize)
		return e
	}); err != nil {
		mu.Lock()
		result.Errors = append(result.Errors, siigoPageErr("Recibos de Pago (RP)", 1, err))
		mu.Unlock()
		return nil
	}
	if err := h.savePaymentReceipts(page1.Results, dateStart, dateEnd, result, mu); err != nil {
		return err
	}

	totalPages := (page1.Pagination.TotalResults + siigoPageSize - 1) / siigoPageSize
	if totalPages <= 1 {
		return nil
	}

	dbErrs := make([]error, totalPages-1)
	var wg sync.WaitGroup
	for page := 2; page <= totalPages; page++ {
		wg.Add(1)
		go func(page int) {
			defer wg.Done()
			var resp *siigopkg.PaymentReceiptListResponse
			if err := withRetry(siigoRetryAttempts, func() error {
				sem <- struct{}{}
				defer func() { <-sem }()
				var e error
				resp, e = client.GetPaymentReceipts(dateStart, dateEnd, page, siigoPageSize)
				return e
			}); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, siigoPageErr("Recibos de Pago (RP)", page, err))
				mu.Unlock()
				return
			}
			dbErrs[page-2] = h.savePaymentReceipts(resp.Results, dateStart, dateEnd, result, mu)
		}(page)
	}
	wg.Wait()
	return errors.Join(dbErrs...)
}

func (h *SiigoHandler) savePaymentReceipts(receipts []siigopkg.PaymentReceipt, dateStart, dateEnd string, result *domain.SiigoSyncResult, mu *sync.Mutex) error {
	for _, pr := range receipts {
		if pr.Date < dateStart || pr.Date > dateEnd {
			continue
		}
		itemDescs := make([]string, 0, len(pr.Items))
		for _, it := range pr.Items {
			if s := strings.TrimSpace(it.Description); s != "" {
				itemDescs = append(itemDescs, s)
			}
		}
		t := &domain.Transaction{
			Date:         pr.Date,
			Description:  pr.Name,
			Reference:    pr.Name,
			Category:     categorizePurchase(itemDescs, pr.Provider.Name),
			Type:         domain.TypeEgreso,
			Amount:       voucherTotal(pr.Total, pr.Items, "Credit"),
			Status:       domain.StatusCompleted,
			Detail:       pr.Name + ifNonEmpty(" · ", strings.Join(itemDescs, " | ")),
			Source:       domain.SourceSIIGO,
			ExternalID:   fmt.Sprintf("siigo-rp-%s", pr.ID),
			IsProjection: false,
		}
		inserted, err := h.store.ImportTransaction(t)
		if err != nil {
			return fmt.Errorf("error al guardar recibo de pago %s: %w", pr.ID, err)
		}
		mu.Lock()
		if inserted {
			result.PaymentReceiptsImported++
		} else {
			result.Updated++
		}
		mu.Unlock()
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// voucherTotal sums items whose account.movement matches the given side.
// Falls back to the top-level total only when no items match.
func voucherTotal(total float64, items []siigopkg.VoucherItem, movement string) float64 {
	var sum float64
	for _, it := range items {
		if it.Account.Movement == movement {
			sum += it.Value
		}
	}
	if sum != 0 {
		return sum
	}
	return total
}

func firstPaymentDueDate(payments []siigopkg.PaymentTerm) string {
	for _, p := range payments {
		if p.DueDate != "" {
			return p.DueDate
		}
	}
	return ""
}

func ifNonEmpty(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}

func invoiceStatus(balance, total float64) domain.TransactionStatus {
	if balance <= 0 {
		return domain.StatusCompleted
	}
	if balance < total {
		return domain.StatusPartial
	}
	return domain.StatusPending
}

var purchaseCategoryKeywords = []struct {
	keywords []string
	category string
}{
	{[]string{"nomina", "nómina", "salario", "sueldo", "empleado", "personal", "contrato laboral", "honorario", "prestacion", "prestación"}, "Personal"},
	{[]string{"software", "tecnologia", "tecnología", "hosting", "servidor", "licencia", "cloud", "aws", "google", "microsoft", "internet", "informatica", "informática", "sistema"}, "Tecnología"},
	{[]string{"publicidad", "marketing", "pauta", "redes sociales", "anuncio", "promocion", "promoción", "agencia", "diseño", "branding"}, "Marketing"},
	{[]string{"arriendo", "arrendamiento", "alquiler", "oficina", "bodega", "local", "inmueble"}, "Arriendo"},
	{[]string{"transporte", "flete", "logistica", "logística", "mensajeria", "mensajería", "envio", "envío", "courier"}, "Logística"},
	{[]string{"servicio publico", "servicio público", "agua", "luz", "energia", "energía", "gas", "telefono", "teléfono", "celular"}, "Servicios Públicos"},
	{[]string{"legal", "juridico", "jurídico", "abogado", "notaria", "notaría", "contrato", "escritura"}, "Legal"},
	{[]string{"contador", "contabilidad", "auditoria", "auditoría", "revisor fiscal", "impuesto", "declaracion", "declaración", "dian"}, "Contabilidad e Impuestos"},
	{[]string{"seguro", "poliza", "póliza", "arl", "eps", "pension", "pensión"}, "Seguros y Beneficios"},
	{[]string{"mantenimiento", "reparacion", "reparación", "aseo", "limpieza", "vigilancia", "seguridad"}, "Mantenimiento"},
	{[]string{"financiero", "bancario", "banco", "credito", "crédito", "prestamo", "préstamo", "interes", "interés", "comision", "comisión bancaria"}, "Finanzas"},
}

var invoiceCategoryKeywords = []struct {
	keywords []string
	category string
}{
	{[]string{"consultoria", "consultoría", "asesor", "servicio profesional"}, "Consultoría"},
	{[]string{"software", "licencia", "sistema", "desarrollo", "tecnologia", "tecnología"}, "Tecnología"},
	{[]string{"producto", "mercancia", "mercancía", "venta de producto"}, "Ventas - Producto"},
	{[]string{"mantenimiento", "soporte"}, "Mantenimiento"},
}

func categorizePurchase(itemDescriptions []string, providerName string) string {
	combined := strings.ToLower(strings.Join(append(itemDescriptions, providerName), " "))
	for _, rule := range purchaseCategoryKeywords {
		for _, kw := range rule.keywords {
			if strings.Contains(combined, kw) {
				return rule.category
			}
		}
	}
	return "Gastos Operativos"
}

func categorizeInvoice(itemDescriptions []string, customerName string) string {
	combined := strings.ToLower(strings.Join(append(itemDescriptions, customerName), " "))
	for _, rule := range invoiceCategoryKeywords {
		for _, kw := range rule.keywords {
			if strings.Contains(combined, kw) {
				return rule.category
			}
		}
	}
	return "Ventas"
}
