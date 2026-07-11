package domain

import "time"

type TransactionType string
type TransactionStatus string
type TransactionSource string
type UserRole string

const (
	TypeIngreso TransactionType = "Ingreso"
	TypeEgreso  TransactionType = "Egreso"

	StatusCompleted TransactionStatus = "Completado"
	StatusPartial   TransactionStatus = "Parcial"
	StatusPending   TransactionStatus = "Pendiente"
	StatusCancelled TransactionStatus = "Anulado"

	SourceSIIGO  TransactionSource = "Siigo"
	SourceManual TransactionSource = "Manual"

	RoleAdmin      UserRole = "ADMINISTRADOR"
	RoleTreasury   UserRole = "TESORERÍA"
	RoleManagement UserRole = "GESTIÓN"
	RoleReadOnly   UserRole = "CONSULTA"
)

type Transaction struct {
	ID           string            `json:"id"`
	Date         string            `json:"date"`
	Description  string            `json:"description"`
	Category     string            `json:"category"`
	Type         TransactionType   `json:"type"`
	Amount       float64           `json:"amount"`
	Status       TransactionStatus `json:"status"`
	Reference    string            `json:"reference,omitempty"`
	Detail       string            `json:"detail,omitempty"`
	Source       TransactionSource `json:"source"`
	ExternalID   string            `json:"externalId,omitempty"`
	IsProjection bool              `json:"isProjection"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

type Invoice struct {
	ID                     string            `json:"id"`
	ExternalID             string            `json:"externalId"`
	Source                 string            `json:"source"`
	IsProjection           bool              `json:"isProjection"`
	Reference              string            `json:"reference,omitempty"`
	Prefix                 string            `json:"prefix,omitempty"`
	Number                 int               `json:"number,omitempty"`
	Date                   string            `json:"date"`
	DueDate                string            `json:"dueDate,omitempty"`
	CustomerIdentification string            `json:"customerIdentification,omitempty"`
	CustomerName           string            `json:"customerName,omitempty"`
	Total                  float64           `json:"total"`
	Balance                float64           `json:"balance"`
	Status                 TransactionStatus `json:"status"`
	Category               string            `json:"category"`
	Detail                 string            `json:"detail,omitempty"`
	SyncedAt               time.Time         `json:"syncedAt"`
	CreatedAt              time.Time         `json:"createdAt"`
	UpdatedAt              time.Time         `json:"updatedAt"`
}

type Purchase struct {
	ID                     string            `json:"id"`
	ExternalID             string            `json:"externalId"`
	Source                 string            `json:"source"`
	IsProjection           bool              `json:"isProjection"`
	Reference              string            `json:"reference,omitempty"`
	Prefix                 string            `json:"prefix,omitempty"`
	Number                 int               `json:"number,omitempty"`
	Date                   string            `json:"date"`
	DueDate                string            `json:"dueDate,omitempty"`
	ProviderIdentification string            `json:"providerIdentification,omitempty"`
	ProviderName           string            `json:"providerName,omitempty"`
	Total                  float64           `json:"total"`
	Balance                float64           `json:"balance"`
	Status                 TransactionStatus `json:"status"`
	Category               string            `json:"category"`
	Detail                 string            `json:"detail,omitempty"`
	SyncedAt               time.Time         `json:"syncedAt"`
	CreatedAt              time.Time         `json:"createdAt"`
	UpdatedAt              time.Time         `json:"updatedAt"`
}

type User struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	Role         UserRole   `json:"role"`
	Password     string     `json:"-"`
	MicrosoftOID string     `json:"microsoftOid,omitempty"`
	Active       bool       `json:"active"`
	LastLoginAt  *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type Category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type StatCard struct {
	Title      string  `json:"title"`
	Value      float64 `json:"value"`
	Change     string  `json:"change"`
	IsPositive bool    `json:"isPositive"`
	TrendText  string  `json:"trendText"`
	Icon       string  `json:"icon"`
}

type ChartDataPoint struct {
	Name     string  `json:"name"`
	Ingresos float64 `json:"ingresos"`
	Egresos  float64 `json:"egresos"`
	Saldo    float64 `json:"saldo"`
}

type PieSlice struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type Alert struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"` // "danger" | "warning" | "success"
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	DueDate     string  `json:"dueDate"`
}

type NotificationItem struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	Date        string  `json:"date"`
	DaysOverdue int     `json:"daysOverdue"` // negative = days until due
	Urgency     string  `json:"urgency"`     // "overdue" | "due-soon" | "upcoming"
}

type NotificationSummary struct {
	Count    int                `json:"count"`
	Gastos   []NotificationItem `json:"gastos"`
	Ingresos []NotificationItem `json:"ingresos"`
}

type WeeklyComparison struct {
	Week     int     `json:"week"`
	Ingresos float64 `json:"ingresos"`
	Egresos  float64 `json:"egresos"`
}

type DashboardSummary struct {
	Stats        []StatCard         `json:"stats"`
	NetFlow      float64            `json:"netFlow"`
	MonthIncome  float64            `json:"monthIncome"`
	MonthExpense float64            `json:"monthExpense"`
	ChartData    []ChartDataPoint   `json:"chartData"`
	ExpensePie   []PieSlice         `json:"expensePie"`
	Alerts       []Alert            `json:"alerts"`
	WeeklyData   []WeeklyComparison `json:"weeklyData"`
}

type CashFlowDay struct {
	Label    string  `json:"label"`
	Date     int     `json:"date"`
	Ingresos float64 `json:"ingresos"`
	Egresos  float64 `json:"egresos"`
}

type CashFlowSummary struct {
	Days             []CashFlowDay `json:"days"`
	Balance          float64       `json:"balance"`
	ProjectedBalance float64       `json:"projectedBalance"`
	ProjectedChange  float64       `json:"projectedChange"`
	Alerts           []Alert       `json:"alerts"`
}

type PeriodData struct {
	Transactions []*Transaction `json:"transactions"`
	Invoices     []*Invoice     `json:"invoices"`
	Purchases    []*Purchase    `json:"purchases"`
}

type ProjectionPoint struct {
	Day     string  `json:"day"`
	Balance float64 `json:"val"`
	Deficit float64 `json:"deficit"`
}

type ProjectionAlert struct {
	ID          string  `json:"id"`
	Icon        string  `json:"icon"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	DueDate     string  `json:"dueDate"`
	Amount      float64 `json:"amount"`
	Color       string  `json:"color"`
}

type ProjectionSummary struct {
	ChartData         []ProjectionPoint `json:"chartData"`
	ProjectedIncome   float64           `json:"projectedIncome"`
	ProjectedExpenses float64           `json:"projectedExpenses"`
	EstimatedBalance  float64           `json:"estimatedBalance"`
	Alerts            []ProjectionAlert `json:"alerts"`
}

type ReportDataPoint struct {
	Name     string  `json:"name"`
	Ingresos float64 `json:"ingresos"`
	Egresos  float64 `json:"egresos"`
}

type CategoryRow struct {
	Category   string  `json:"category"`
	Amount     float64 `json:"amount"`
	Prev       float64 `json:"prev"`       // same period prior cycle
	Change     float64 `json:"change"`     // % change vs prev
	IsPositive bool    `json:"isPositive"` // true = spending decreased
}

type AnnualProjection struct {
	ProjectedClose float64 `json:"projectedClose"`
	Probability    float64 `json:"probability"`
	InsightText    string  `json:"insightText"`
}

type ReportSummary struct {
	CashFlowChart     []ReportDataPoint `json:"cashFlowChart"`
	CategoryBreakdown []PieSlice        `json:"categoryBreakdown"`
	CategoryTable     []CategoryRow     `json:"categoryTable"`
	Annual            AnnualProjection  `json:"annual"`
	ComplianceRate    float64           `json:"complianceRate"`
}

type Settings struct {
	BaseCurrency     string `json:"baseCurrency"`
	AutoExchangeRate bool   `json:"autoExchangeRate"`
	Theme            string `json:"theme"`
}

type BudgetLine struct {
	Category string  `json:"category"`
	Monthly  float64 `json:"monthly"`
}

type SiigoConfig struct {
	UserName  string    `json:"userName"`
	PartnerID string    `json:"partnerId"`
	Connected bool      `json:"connected"`
	LastSync  time.Time `json:"lastSync,omitempty"`
	TokenExp  time.Time `json:"tokenExp,omitempty"`
	AccessKey string    `json:"-"`
}

// BankAccount is a single named bank balance entry (e.g. "Bancolombia" → 15M).
type BankAccount struct {
	Label  string  `json:"label"`
	Amount float64 `json:"amount"`
}

type BankBalance struct {
	Accounts  []BankAccount `json:"accounts"`
	Amount    float64       `json:"amount"` // total — sum of all account amounts
	UpdatedAt time.Time     `json:"updatedAt"`
	UpdatedBy string        `json:"updatedBy"`
}

type SiigoConnectRequest struct {
	UserName  string `json:"userName"`
	AccessKey string `json:"accessKey"`
	PartnerID string `json:"partnerId"`
}

type SiigoSyncMode string

const (
	SyncModeIncremental SiigoSyncMode = "incremental" // rolling last 90 days (default)
	SyncModeBootstrap   SiigoSyncMode = "bootstrap"   // full history from DateStart
	SyncModeReconcile   SiigoSyncMode = "reconcile"   // re-scan from earliest known record
)

type SiigoSyncRequest struct {
	Mode      SiigoSyncMode `json:"mode"`
	DateStart string        `json:"dateStart"` // YYYY-MM-DD — required for bootstrap
	DateEnd   string        `json:"dateEnd"`   // YYYY-MM-DD — defaults to today
}

type SiigoSyncResult struct {
	Mode                    SiigoSyncMode `json:"mode"`
	DateStart               string        `json:"dateStart"`
	DateEnd                 string        `json:"dateEnd"`
	InvoicesImported        int           `json:"invoicesImported"`
	PurchasesImported       int           `json:"purchasesImported"`
	VouchersImported        int           `json:"vouchersImported"`
	PaymentReceiptsImported int           `json:"paymentReceiptsImported"`
	Updated                 int           `json:"updated"`
	Errors                  []string      `json:"errors,omitempty"`
}

type ActivityLog struct {
	ID        string    `json:"id"`
	UserName  string    `json:"userName"`
	Initial   string    `json:"initial"`
	Action    string    `json:"action"`
	Module    string    `json:"module"`
	Timestamp time.Time `json:"timestamp"`
	Color     string    `json:"color"`
}

type SearchDocument struct {
	ID             string  `json:"id"`
	DocType        string  `json:"docType"`
	Reference      string  `json:"reference"`
	Date           string  `json:"date"`
	DueDate        string  `json:"dueDate,omitempty"`
	Description    string  `json:"description"`
	Detail         string  `json:"detail,omitempty"`
	Category       string  `json:"category"`
	Type           string  `json:"type,omitempty"` // Ingreso / Egreso (transactions)
	Amount         float64 `json:"amount"`
	Balance        float64 `json:"balance,omitempty"` // invoices / purchases
	Status         string  `json:"status"`
	Counterparty   string  `json:"counterparty,omitempty"`   // customer / provider name
	CounterpartyID string  `json:"counterpartyId,omitempty"` // customer / provider identification
	Source         string  `json:"source,omitempty"`
	Prefix         string  `json:"prefix,omitempty"`
	Number         int     `json:"number,omitempty"`
	IsProjection   bool    `json:"isProjection,omitempty"`
	ExternalID     string  `json:"externalId,omitempty"`
	SyncedAt       string  `json:"syncedAt,omitempty"`
	CreatedAt      string  `json:"createdAt,omitempty"`
	UpdatedAt      string  `json:"updatedAt,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type TransactionListResponse struct {
	Data       []Transaction `json:"data"`
	Total      int           `json:"total"`
	Page       int           `json:"page"`
	Limit      int           `json:"limit"`
	TotalPages int           `json:"totalPages"`
}

type InvoiceListResponse struct {
	Data       []Invoice `json:"data"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	Limit      int       `json:"limit"`
	TotalPages int       `json:"totalPages"`
}

type PurchaseListResponse struct {
	Data       []Purchase `json:"data"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	Limit      int        `json:"limit"`
	TotalPages int        `json:"totalPages"`
}

// ── Aggregation result types (returned by focused DB queries) ─────────────────

type MonthlyTotal struct {
	Year    int
	Month   time.Month
	Income  float64
	Expense float64
}

type DailyTotal struct {
	Date     string // YYYY-MM-DD
	Ingresos float64
	Egresos  float64
}

type CategoryTotal struct {
	Category string
	Amount   float64
}

type TransactionSummary struct {
	TotalBalance   float64 `json:"totalBalance"`
	TotalIncome    float64 `json:"totalIncome"`
	TotalExpense   float64 `json:"totalExpense"`
	MonthlyIncome  float64 `json:"monthlyIncome"`
	MonthlyExpense float64 `json:"monthlyExpense"`
}

type SimulateRequest struct {
	SalesGrowth  float64 `json:"salesGrowth"`
	PaymentDelay int     `json:"paymentDelay"`
}

type SimulateResponse struct {
	ProjectedBalance float64 `json:"projectedBalance"`
	Impact           float64 `json:"impact"`
	RiskLevel        string  `json:"riskLevel"`
}
