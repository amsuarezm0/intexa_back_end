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
	StatusCancelled TransactionStatus = "Cancelado"

	SourceSIIGO  TransactionSource = "Siigo"
	SourceManual TransactionSource = "Manual"

	RoleAdmin    UserRole = "ADMINISTRADOR"
	RoleTreasury UserRole = "TESORERÍA"
	RoleReadOnly UserRole = "CONSULTA"
)

type Transaction struct {
	ID           string            `json:"id"`
	Date         string            `json:"date"`
	Description  string            `json:"description"`
	Category     string            `json:"category"`
	Type         TransactionType   `json:"type"`
	Amount       float64           `json:"amount"`
	Balance      float64           `json:"balance,omitempty"` // remaining unpaid amount (Siigo)
	Status       TransactionStatus `json:"status"`
	Reference    string            `json:"reference,omitempty"`
	Detail       string            `json:"detail,omitempty"`
	Source       TransactionSource `json:"source"`
	ExternalID   string            `json:"externalId,omitempty"`
	ParentID     string            `json:"parentId,omitempty"` // set on payment-term projections
	IsProjection bool              `json:"isProjection"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
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
	ProjectedBalance float64       `json:"projectedBalance"`
	ProjectedChange  float64       `json:"projectedChange"`
	Alerts           []Alert       `json:"alerts"`
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

type BankBalance struct {
	Amount    float64   `json:"amount"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy"`
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
	Mode              SiigoSyncMode `json:"mode"`
	DateStart         string        `json:"dateStart"`
	DateEnd           string        `json:"dateEnd"`
	InvoicesImported  int           `json:"invoicesImported"`
	PurchasesImported int           `json:"purchasesImported"`
	Updated           int           `json:"updated"`
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

type TransactionSummary struct {
	TotalBalance   float64 `json:"totalBalance"`
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
