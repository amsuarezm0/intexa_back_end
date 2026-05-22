package siigo

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
	Page      int `json:"page"`
	PerPage   int `json:"per_page"`
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
	ID           int             `json:"id"`
	Document     DocumentRef     `json:"document"`
	Prefix       string          `json:"prefix"`
	Number       int             `json:"number"`
	Name         string          `json:"name"`
	Date         string          `json:"date"`         // YYYY-MM-DD
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

type InvoiceItem struct {
	Code        string       `json:"code"`
	Description string       `json:"description"`
	Quantity    float64      `json:"quantity"`
	Discount    float64      `json:"discount"`
	Price       float64      `json:"price"`
	Taxed       bool         `json:"taxed"`
	Taxes       []TaxRef     `json:"taxes"`
	Total       float64      `json:"total"`
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
	ID           int             `json:"id"`
	Document     DocumentRef     `json:"document"`
	Prefix       string          `json:"prefix"`
	Number       int             `json:"number"`
	Date         string          `json:"date"`
	DueDate      string          `json:"due_date"`
	Provider     PurchaseProvider `json:"provider"`
	Total        float64         `json:"total"`
	Balance      float64         `json:"balance"`
	Observations string          `json:"observations"`
	Payments     []PaymentTerm   `json:"payments"`
	Items        []InvoiceItem   `json:"items"`
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

// Customer

type Customer struct {
	ID             string `json:"id"`
	PersonType     string `json:"person_type"`
	IDType         string `json:"id_type"`
	Identification string `json:"identification"`
	Name           string `json:"name"`
	CommercialName string `json:"commercial_name"`
	Active         bool   `json:"active"`
	Email          string `json:"email"`
	Phone          string `json:"phone"`
}

// Product

type Product struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	AccountGroup int    `json:"account_group"`
	Type        string  `json:"type"`
	TaxClassification string `json:"tax_classification"`
	Active      bool    `json:"active"`
	Price       []ProductPrice `json:"price"`
}

type ProductPrice struct {
	CurrencyName string  `json:"currency_name"`
	Value        float64 `json:"value"`
}
