package handler

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
	siigopkg "github.com/intexa/arca-api/internal/siigo"
)

type SiigoHandler struct {
	store  repository.Store
	mu     sync.Mutex
	client *siigopkg.Client
}

func NewSiigoHandler(store repository.Store) *SiigoHandler {
	return &SiigoHandler{store: store}
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
	h.mu.Lock()
	client := h.client
	h.mu.Unlock()

	if client == nil || !client.IsConnected() {
		jsonError(w, "not connected to Siigo — call /siigo/connect first", http.StatusConflict)
		return
	}

	var req domain.SiigoSyncRequest
	if err := decode(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.DateEnd == "" {
		req.DateEnd = time.Now().Format("2006-01-02")
	}
	if req.DateStart == "" {
		req.DateStart = time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	}

	result := domain.SiigoSyncResult{}

	invoiceResp, err := client.GetInvoices(req.DateStart, req.DateEnd, 1, 100)
	if err != nil {
		jsonError(w, fmt.Sprintf("failed to fetch invoices: %v", err), http.StatusBadGateway)
		return
	}
	for _, inv := range invoiceResp.Results {
		extID := fmt.Sprintf("siigo-inv-%d", inv.ID)
		exists, err := h.store.ExternalIDExists(extID)
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if exists {
			result.Skipped++
			continue
		}
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
			Reference: fmt.Sprintf("%s-%d", inv.Prefix, inv.Number),
			Source: domain.SourceSIIGO, ExternalID: extID,
			CreatedAt: parseSiigoDate(inv.Date),
		}
		if err := h.store.ImportTransaction(t); err != nil {
			jsonError(w, "failed to save invoice", http.StatusInternalServerError)
			return
		}
		result.InvoicesImported++
	}

	purchaseResp, err := client.GetPurchases(req.DateStart, req.DateEnd, 1, 100)
	if err != nil {
		jsonError(w, fmt.Sprintf("failed to fetch purchases: %v", err), http.StatusBadGateway)
		return
	}
	for _, pur := range purchaseResp.Results {
		extID := fmt.Sprintf("siigo-pur-%d", pur.ID)
		exists, err := h.store.ExternalIDExists(extID)
		if err != nil {
			jsonError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if exists {
			result.Skipped++
			continue
		}
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
			Reference: fmt.Sprintf("%s-%d", pur.Prefix, pur.Number),
			Source: domain.SourceSIIGO, ExternalID: extID,
			CreatedAt: parseSiigoDate(pur.Date),
		}
		if err := h.store.ImportTransaction(t); err != nil {
			jsonError(w, "failed to save purchase", http.StatusInternalServerError)
			return
		}
		result.PurchasesImported++
	}

	h.store.UpdateSiigoLastSync(time.Now()) //nolint
	h.store.AddActivityLog(domain.ActivityLog{  //nolint
		UserName: "Sistema", Initial: "SI",
		Action: fmt.Sprintf("Sync Siigo (+%d FV, +%d FC)", result.InvoicesImported, result.PurchasesImported),
		Module: "Integración", Color: "bg-green-500",
	})
	jsonOK(w, result)
}

func invoiceStatus(balance float64) domain.TransactionStatus {
	if balance == 0 {
		return domain.StatusCompleted
	}
	return domain.StatusPending
}

func parseSiigoDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Now()
	}
	return t
}
