package siigo

import "encoding/json"

// Auth

type SignInRequest struct {
	UserName  string `json:"username"`
	AccessKey string `json:"access_key"`
}

type SignInResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// Pagination wrapper used by list endpoints

type Pagination struct {
	Page         int `json:"page"`
	PerPage      int `json:"per_page"`
	TotalResults int `json:"total_results"`
}

type InvoiceListResponse struct {
	Pagination Pagination `json:"pagination"`
	Results    []Invoice  `json:"results"`
}

type PurchaseListResponse struct {
	Pagination Pagination `json:"pagination"`
	Results    []Purchase `json:"results"`
}

// Invoice (venta / Ingreso)

type Invoice struct {
	ID           string          `json:"id"`
	Document     DocumentRef     `json:"document"`
	Prefix       string          `json:"prefix"`
	Number       int             `json:"number"`
	Name         string          `json:"name"`
	Date         string          `json:"date"` // YYYY-MM-DD
	DueDate      string          `json:"due_date"`
	Customer     InvoiceCustomer `json:"customer"`
	Seller       int             `json:"seller"`
	Total        float64         `json:"total"`
	Balance      float64         `json:"balance"`
	Observations string          `json:"observations"`
	Payments     []PaymentTerm   `json:"payments"`
	Items        []InvoiceItem   `json:"items"`
	Stamp        *Stamp          `json:"stamp,omitempty"`
}

type DocumentRef struct {
	ID int `json:"id"`
}

type InvoiceCustomer struct {
	PersonType     string `json:"person_type"`
	IDType         string `json:"id_type"`
	Identification string `json:"identification"`
	BranchOffice   int    `json:"branch_office"`
	Name           string `json:"name"`
	CommercialName string `json:"commercial_name"`
	Active         bool   `json:"active"`
}

type PaymentTerm struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	DueDate string  `json:"due_date"`
	Value   float64 `json:"value"`
}

// Discount handles both a plain number and an object from Siigo.
type Discount struct {
	Percentage float64
	Value      float64
}

func (d *Discount) UnmarshalJSON(b []byte) error {
	// plain number (purchases)
	var n float64
	if err := json.Unmarshal(b, &n); err == nil {
		d.Percentage = n
		return nil
	}
	// object (invoices)
	var obj struct {
		Percentage float64 `json:"percentage"`
		Value      float64 `json:"value"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	d.Percentage = obj.Percentage
	d.Value = obj.Value
	return nil
}

type InvoiceItem struct {
	Code        string   `json:"code"`
	Description string   `json:"description"`
	Quantity    float64  `json:"quantity"`
	Discount    Discount `json:"discount"`
	Price       float64  `json:"price"`
	Taxed       bool     `json:"taxed"`
	Taxes       []TaxRef `json:"taxes"`
	Total       float64  `json:"total"`
}

type TaxRef struct {
	ID int `json:"id"`
}

type Stamp struct {
	SendBillStatus string `json:"send_bill_status"`
	UUID           string `json:"uuid"`
}

// Purchase (compra / Egreso)

type Purchase struct {
	ID           string           `json:"id"`
	Document     DocumentRef      `json:"document"`
	Prefix       string           `json:"prefix"`
	Number       int              `json:"number"`
	Name         string           `json:"name"`
	Date         string           `json:"date"`
	DueDate      string           `json:"due_date"`
	Provider     PurchaseProvider `json:"provider"`
	Total        float64          `json:"total"`
	Balance      float64          `json:"balance"`
	Observations string           `json:"observations"`
	Payments     []PaymentTerm    `json:"payments"`
	Items        []InvoiceItem    `json:"items"`
}

type PurchaseProvider struct {
	PersonType     string `json:"person_type"`
	IDType         string `json:"id_type"`
	Identification string `json:"identification"`
	BranchOffice   int    `json:"branch_office"`
	Name           string `json:"name"`
	CommercialName string `json:"commercial_name"`
	Active         bool   `json:"active"`
}

// Voucher (RC — comprobante de cobro, ingreso real de caja)

type VoucherListResponse struct {
	Pagination Pagination `json:"pagination"`
	Results    []Voucher  `json:"results"`
}

type Voucher struct {
	ID           string          `json:"id"`
	Prefix       string          `json:"prefix"`
	Number       int             `json:"number"`
	Name         string          `json:"name"`
	Date         string          `json:"date"` // YYYY-MM-DD
	Customer     VoucherCustomer `json:"customer"`
	Total        float64         `json:"total"`
	Observations string          `json:"observations"`
	Items        []VoucherItem   `json:"items"`
}

type VoucherCustomer struct {
	Identification string `json:"identification"`
	Name           string `json:"name"`
}

type VoucherItem struct {
	Description string         `json:"description"`
	Value       float64        `json:"value"`
	Account     VoucherAccount `json:"account"`
}

type VoucherAccount struct {
	Movement string `json:"movement"` // "Debit" | "Credit"
}

// PaymentReceipt (RP — comprobante de egreso, pago real a proveedor)

type PaymentReceiptListResponse struct {
	Pagination Pagination       `json:"pagination"`
	Results    []PaymentReceipt `json:"results"`
}

type PaymentReceipt struct {
	ID           string                 `json:"id"`
	Prefix       string                 `json:"prefix"`
	Number       int                    `json:"number"`
	Name         string                 `json:"name"`
	Date         string                 `json:"date"` // YYYY-MM-DD
	Provider     PaymentReceiptProvider `json:"provider"`
	Total        float64                `json:"total"`
	Observations string                 `json:"observations"`
	Items        []VoucherItem          `json:"items"`
}

type PaymentReceiptProvider struct {
	Identification string `json:"identification"`
	Name           string `json:"name"`
}

