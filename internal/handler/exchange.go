package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type ExchangeRateHandler struct {
	mu        sync.RWMutex
	rates     map[string]float64
	fetchedAt time.Time
}

func NewExchangeRateHandler() *ExchangeRateHandler {
	return &ExchangeRateHandler{}
}

type erAPIResponse struct {
	Result string             `json:"result"`
	Rates  map[string]float64 `json:"rates"`
}

// GET /api/v1/exchange-rates
func (h *ExchangeRateHandler) GetRates(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	rates := h.rates
	stale := rates == nil || time.Since(h.fetchedAt) > 24*time.Hour
	h.mu.RUnlock()

	if stale {
		fresh, err := fetchExchangeRates()
		if err != nil {
			if rates != nil {
				jsonOK(w, rates) // serve stale rather than error
				return
			}
			jsonError(w, fmt.Sprintf("failed to fetch exchange rates: %v", err), http.StatusBadGateway)
			return
		}
		h.mu.Lock()
		h.rates = fresh
		h.fetchedAt = time.Now()
		h.mu.Unlock()
		rates = fresh
	}

	jsonOK(w, rates)
}

func fetchExchangeRates() (map[string]float64, error) {
	resp, err := http.Get("https://open.er-api.com/v6/latest/USD")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data erAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if data.Result != "success" {
		return nil, fmt.Errorf("exchange rate API returned: %s", data.Result)
	}
	return data.Rates, nil
}
