package siigo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	BaseURL      = "https://api.siigo.com"
	AuthPath     = "/auth"
	InvoicePath  = "/v1/invoices"
	PurchasePath = "/v1/purchases"
	CustomerPath = "/v1/customers"
	ProductPath  = "/v1/products"
)

type Client struct {
	mu         sync.Mutex
	userName   string
	accessKey  string
	partnerID  string
	token      string
	tokenExp   time.Time
	httpClient *http.Client
}

func NewClient(userName, accessKey, partnerID string) *Client {
	return &Client{
		userName:   userName,
		accessKey:  accessKey,
		partnerID:  partnerID,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Connect authenticates against Siigo and stores the token. Returns an error
// if the credentials are rejected.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refreshToken()
}

func (c *Client) refreshToken() error {
	body, _ := json.Marshal(SignInRequest{
		UserName:  c.userName,
		AccessKey: c.accessKey,
	})

	req, err := http.NewRequest(http.MethodPost, BaseURL+AuthPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Partner-Id", c.partnerID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("siigo auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("siigo auth error %d: %s", resp.StatusCode, string(raw))
	}

	var res SignInResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return fmt.Errorf("siigo auth decode: %w", err)
	}

	c.token = res.AccessToken
	exp := res.ExpiresIn
	if exp == 0 {
		exp = 86400
	}
	c.tokenExp = time.Now().Add(time.Duration(exp) * time.Second)
	return nil
}

func (c *Client) ensureToken() error {
	if c.token == "" || time.Now().After(c.tokenExp.Add(-5*time.Minute)) {
		return c.refreshToken()
	}
	return nil
}

func (c *Client) get(path string, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureToken(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodGet, BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Partner-Id", c.partnerID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("siigo GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("siigo GET %s error %d: %s", path, resp.StatusCode, string(raw))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

// GetInvoices returns sales invoices for the given date range (YYYY-MM-DD).
func (c *Client) GetInvoices(dateStart, dateEnd string, page, pageSize int) (*InvoiceListResponse, error) {
	path := fmt.Sprintf("%s?date_start=%s&date_end=%s&page=%d&page_size=%d",
		InvoicePath, dateStart, dateEnd, page, pageSize)
	var result InvoiceListResponse
	return &result, c.get(path, &result)
}

// GetPurchases returns purchase invoices for the given date range.
func (c *Client) GetPurchases(dateStart, dateEnd string, page, pageSize int) (*PurchaseListResponse, error) {
	path := fmt.Sprintf("%s?date_start=%s&date_end=%s&page=%d&page_size=%d",
		PurchasePath, dateStart, dateEnd, page, pageSize)
	var result PurchaseListResponse
	return &result, c.get(path, &result)
}

// GetCustomers returns a paged list of customers.
func (c *Client) GetCustomers(page, pageSize int) ([]Customer, error) {
	path := fmt.Sprintf("%s?page=%d&page_size=%d", CustomerPath, page, pageSize)
	var result struct {
		Results []Customer `json:"results"`
	}
	return result.Results, c.get(path, &result)
}

// GetProducts returns a paged list of products.
func (c *Client) GetProducts(page, pageSize int) ([]Product, error) {
	path := fmt.Sprintf("%s?page=%d&page_size=%d", ProductPath, page, pageSize)
	var result struct {
		Results []Product `json:"results"`
	}
	return result.Results, c.get(path, &result)
}

// IsConnected reports whether the client holds a valid, non-expired token.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token != "" && time.Now().Before(c.tokenExp)
}

// TokenExpiry returns when the current token expires.
func (c *Client) TokenExpiry() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokenExp
}
