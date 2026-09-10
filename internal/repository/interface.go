package repository

import (
	"time"

	"github.com/intexa/arca-api/internal/domain"
)

// Store is the data-access contract implemented by the Postgres store.
type Store interface {
	// ── Transactions ────────────────────────────────────────────────────────
	GetAllTransactions() ([]*domain.Transaction, error)
	GetTransactionByID(id string) (*domain.Transaction, bool, error)
	CreateTransaction(t *domain.Transaction) error
	// NextManualReference returns the next sequential reference for a manual
	// record, e.g. "MM-000123" for movements ("MM") or "PM-000045" for
	// projections ("PM").
	NextManualReference(prefix string) (string, error)
	ImportTransaction(t *domain.Transaction) (bool, error)
	UpdateTransaction(t *domain.Transaction) (bool, error)
	DeleteTransaction(id string) (bool, error)

	// ── Focused aggregation queries (DB does the work) ───────────────────────
	GetCurrentBalance() (float64, error)
	GetMonthlyTotals(from, to time.Time) ([]domain.MonthlyTotal, error)
	GetDailyTotals(from, to time.Time) ([]domain.DailyTotal, error)
	GetPendingTransactions() ([]*domain.Transaction, error)
	GetPendingProjections(horizon time.Time) ([]*domain.Transaction, error)
	GetCategoryTotals(from, to time.Time, txType domain.TransactionType) ([]domain.CategoryTotal, error)
	GetWeeklyTotals(year int, month time.Month) ([]domain.WeeklyComparison, error)

	// ── Projection periods (custom horizons) ─────────────────────────────────
	GetProjectionPeriods() ([]domain.ProjectionPeriod, error)
	CreateProjectionPeriod(p *domain.ProjectionPeriod) error
	DeleteProjectionPeriod(id string) (bool, error)

	// ── Users ────────────────────────────────────────────────────────────────
	GetUserByEmail(email string) (*domain.User, bool, error)
	GetUserByMicrosoftOID(oid string) (*domain.User, bool, error)
	GetAllUsers() ([]*domain.User, error)
	GetUserByID(id string) (*domain.User, bool, error)
	CreateUser(u *domain.User) error
	UpdateUser(u *domain.User) (bool, error)
	UpdatePassword(userID, hashedPassword string) (bool, error)
	DeleteUser(id string) (bool, error)

	// ── Access control ───────────────────────────────────────────────────────
	IsEmailAllowed(email string) (bool, error) // domain allowlist OR pre-existing user
	GetAllowedDomains() ([]string, error)
	AddAllowedDomain(domain string) error
	RemoveAllowedDomain(domain string) error

	// ── Categories ───────────────────────────────────────────────────────────
	GetCategories() ([]domain.Category, error)
	CreateCategory(c *domain.Category) error

	// ── Settings ─────────────────────────────────────────────────────────────
	GetSettings(userID string) (domain.Settings, error)
	UpdateSettings(userID string, st domain.Settings) error

	// ── Activity logs ────────────────────────────────────────────────────────
	GetActivityLogs() ([]domain.ActivityLog, error)
	AddActivityLog(log domain.ActivityLog) error

	// ── Budgets ──────────────────────────────────────────────────────────────
	GetBudgets() ([]domain.BudgetLine, error)
	SetBudgets(b []domain.BudgetLine) error

	// ── Siigo config ─────────────────────────────────────────────────────────
	GetSiigoConfig() (*domain.SiigoConfig, error)
	SetSiigoConfig(cfg domain.SiigoConfig) error
	UpdateSiigoLastSync(t time.Time) error
	GetEarliestSiigoDate() (string, error)
	GetOldestPendingOrPartialDate() (string, error)

	// ── Invoices (FV — facturas de venta) ────────────────────────────────────────
	GetAllInvoices() ([]*domain.Invoice, error)
	GetPendingInvoices() ([]*domain.Invoice, error) // Pendiente + Parcial only
	GetInvoiceByID(id string) (*domain.Invoice, bool, error)
	UpsertInvoice(inv *domain.Invoice) (bool, error) // true = inserted
	// SetInvoiceSecondaryDueDate writes the agreed payment date — the only
	// manual edit allowed on a Siigo document. Empty clears it.
	SetInvoiceSecondaryDueDate(id, date string) (bool, error)

	// ── Purchases (FC — facturas de compra) ──────────────────────────────────────
	GetAllPurchases() ([]*domain.Purchase, error)
	GetPendingPurchases() ([]*domain.Purchase, error) // Pendiente + Parcial only
	GetPurchaseByID(id string) (*domain.Purchase, bool, error)
	UpsertPurchase(pur *domain.Purchase) (bool, error) // true = inserted
	SetPurchaseSecondaryDueDate(id, date string) (bool, error)

	// ── Customers (terceros sincronizados desde Siigo) ───────────────────────
	GetAllCustomers() ([]*domain.Customer, error)
	GetCustomerByID(id string) (*domain.Customer, bool, error)
	// UpsertCustomer keys on (identification, branch_office) so a sync can never
	// create a second row for the same third party. true = inserted.
	UpsertCustomer(c *domain.Customer) (bool, error)
	// DeactivateCustomersNotSyncedSince flags customers missing from the latest
	// full sync as inactive instead of deleting them, so history that references
	// them stays readable. Returns how many were flagged.
	DeactivateCustomersNotSyncedSince(t time.Time) (int, error)
	// GetCustomerAggregates rolls invoices up by customer identification.
	GetCustomerAggregates() (map[string]domain.CustomerAggregate, error)
	GetInvoicesByCustomer(identification string) ([]*domain.Invoice, error)
	// GetThirdPartyDirectory resolves document counterparties in bulk, keyed by
	// identification+branch office and also by bare identification.
	GetThirdPartyDirectory() (map[string]domain.ThirdParty, error)

	// ── Bank balance ──────────────────────────────────────────────────────────
	GetBankBalance() (*domain.BankBalance, error)
	SetBankBalance(b domain.BankBalance) error

	// ── Cashflow period ───────────────────────────────────────────────────────
	GetPeriodData(from, to time.Time) (*domain.PeriodData, error)

	// ── Search ────────────────────────────────────────────────────────────────
	Search(reference string) ([]domain.SearchDocument, error)
}
