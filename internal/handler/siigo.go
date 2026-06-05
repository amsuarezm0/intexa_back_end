package handler

import (
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
	siigoPageSize     = 100
	incrementalDays   = 90  // rolling window for incremental (days back from today)
	schedulerHour     = 6   // daily sync fires at 06:00 local time
	reconcileDay      = 1   // reconcile fires on the 1st of each month
	bootstrapFallback = 730 // days back when no records exist (2 years)
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
			"mode",               mode,
			"invoices_imported",  result.InvoicesImported,
			"purchases_imported", result.PurchasesImported,
			"updated",            result.Updated,
			"date_start",         dateStart,
			"date_end",           dateEnd,
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

	actor, initial := actorFrom(r)
	result, err := h.runSync(client, req.Mode, dateStart, dateEnd, actor, initial)
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
		// Start from the oldest Pendiente/Parcial transaction so any status
		// changes Siigo has recorded since that date are picked up.
		oldest, err := h.store.GetOldestPendingOrPartialDate()
		if err != nil {
			return "", "", err
		}
		if oldest == "" {
			// No pending transactions — fall back to last 90 days.
			oldest = time.Now().AddDate(0, 0, -incrementalDays).Format("2006-01-02")
		}
		slog.Info("siigo_reconcile_window", "date_start", oldest, "date_end", today)
		return oldest, today, nil

	default: // incremental
		// Always scan the last 90 days from today so status changes and new
		// records within that window are never missed.
		return time.Now().AddDate(0, 0, -incrementalDays).Format("2006-01-02"), today, nil
	}
}

// runSync fetches all pages for the given window and upserts into the store.
func (h *SiigoHandler) runSync(client *siigopkg.Client, mode domain.SiigoSyncMode, dateStart, dateEnd, actor, initial string) (*domain.SiigoSyncResult, error) {
	result := &domain.SiigoSyncResult{Mode: mode, DateStart: dateStart, DateEnd: dateEnd}

	if err := h.syncInvoices(client, dateStart, dateEnd, result); err != nil {
		return nil, err
	}
	if err := h.syncPurchases(client, dateStart, dateEnd, result); err != nil {
		return nil, err
	}

	h.store.UpdateSiigoLastSync(time.Now())    //nolint
	h.store.AddActivityLog(domain.ActivityLog{ //nolint
		UserName: actor, Initial: initial,
		Action: fmt.Sprintf("Sync Siigo [%s] (+%d FV, +%d FC, ~%d actualizados)",
			mode, result.InvoicesImported, result.PurchasesImported, result.Updated),
		Module: "Integración", Color: "bg-green-500",
	})
	slog.Info("siigo_sync_complete",
		"mode",               mode,
		"actor",              actor,
		"invoices_imported",  result.InvoicesImported,
		"purchases_imported", result.PurchasesImported,
		"updated",            result.Updated,
		"date_start",         dateStart,
		"date_end",           dateEnd,
	)
	return result, nil
}

func (h *SiigoHandler) syncInvoices(client *siigopkg.Client, dateStart, dateEnd string, result *domain.SiigoSyncResult) error {
	for page := 1; ; page++ {
		resp, err := client.GetInvoices(dateStart, dateEnd, page, siigoPageSize)
		if err != nil {
			return fmt.Errorf("fetching invoices page %d: %w", page, err)
		}
		for _, inv := range resp.Results {
			if inv.Date < dateStart || inv.Date > dateEnd {
				continue
			}
			desc := inv.Name
			if desc == "" {
				desc = siigoRef(inv.Prefix, inv.Number)
			}
			itemDescs := make([]string, 0, len(inv.Items))
			for _, it := range inv.Items {
				itemDescs = append(itemDescs, strings.TrimSpace(it.Description))
			}
			t := &domain.Transaction{
				Date:        inv.Date,
				Description: desc,
				Category:    categorizeInvoice(itemDescs, ""),
				Type:        domain.TypeIngreso,
				Amount:      inv.Total,
				Balance:     inv.Balance,
				Status:      invoiceStatus(inv.Balance, inv.Total),
				Reference:   desc,
				Detail:      strings.Join(itemDescs, " | "),
				Source:      domain.SourceSIIGO,
				ExternalID:  fmt.Sprintf("siigo-inv-%s", inv.ID),
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
			if err := h.syncPaymentTerms(t.ID, desc, domain.TypeIngreso, t.Category, inv.Payments, "siigo-inv-"+inv.ID, result); err != nil {
				return fmt.Errorf("saving payment terms for invoice %s: %w", inv.ID, err)
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
			if pur.Date < dateStart || pur.Date > dateEnd {
				continue
			}
			desc := pur.Name
			if desc == "" {
				desc = siigoRef(pur.Prefix, pur.Number)
			}
			itemDescs := make([]string, 0, len(pur.Items))
			for _, it := range pur.Items {
				itemDescs = append(itemDescs, strings.TrimSpace(it.Description))
			}
			t := &domain.Transaction{
				Date:        pur.Date,
				Description: desc,
				Category:    categorizePurchase(itemDescs, ""),
				Type:        domain.TypeEgreso,
				Amount:      pur.Total,
				Balance:     pur.Balance,
				Status:      invoiceStatus(pur.Balance, pur.Total),
				Reference:   desc,
				Detail:      strings.Join(itemDescs, " | "),
				Source:      domain.SourceSIIGO,
				ExternalID:  fmt.Sprintf("siigo-pur-%s", pur.ID),
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
			if err := h.syncPaymentTerms(t.ID, desc, domain.TypeEgreso, t.Category, pur.Payments, "siigo-pur-"+pur.ID, result); err != nil {
				return fmt.Errorf("saving payment terms for purchase %s: %w", pur.ID, err)
			}
		}
		if page*siigoPageSize >= resp.Pagination.TotalResults {
			break
		}
	}
	return nil
}

// syncPaymentTerms upserts one projected transaction per PaymentTerm when the
// invoice/purchase has more than one installment (multi-cuota plan).
func (h *SiigoHandler) syncPaymentTerms(
	parentID, parentDesc string,
	txType domain.TransactionType,
	category string,
	terms []siigopkg.PaymentTerm,
	extPrefix string,
	result *domain.SiigoSyncResult,
) error {
	if len(terms) <= 1 {
		return nil
	}
	for i, pt := range terms {
		proj := &domain.Transaction{
			Date:         pt.DueDate,
			Description:  fmt.Sprintf("%s — cuota %d/%d", parentDesc, i+1, len(terms)),
			Category:     category,
			Type:         txType,
			Amount:       pt.Value,
			Status:       domain.StatusPending,
			Source:       domain.SourceSIIGO,
			ExternalID:   fmt.Sprintf("%s-pterm-%d", extPrefix, pt.ID),
			ParentID:     parentID,
			IsProjection: true,
		}
		if _, err := h.store.ImportTransaction(proj); err != nil {
			return err
		}
		result.Updated++
	}
	return nil
}

// siigoRef builds a human-readable document reference from prefix and number.
// When prefix is empty (Siigo omits it on some document types) it falls back
// to just the number so we never produce a leading "-" in the UI.
func siigoRef(prefix string, number int) string {
	if prefix == "" {
		return fmt.Sprintf("%d", number)
	}
	return fmt.Sprintf("%s-%d", prefix, number)
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

// categoryKeywords maps lowercase keywords found in item descriptions or provider/customer
// names to a canonical category name.
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
