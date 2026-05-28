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
	ImportTransaction(t *domain.Transaction) (bool, error)
	UpdateTransaction(t *domain.Transaction) (bool, error)
	DeleteTransaction(id string) (bool, error)

	// ── Users ────────────────────────────────────────────────────────────────
	GetUserByEmail(email string) (*domain.User, bool, error)
	GetUserByMicrosoftOID(oid string) (*domain.User, bool, error)
	GetAllUsers() ([]*domain.User, error)
	GetUserByID(id string) (*domain.User, bool, error)
	CreateUser(u *domain.User) error
	UpdateUser(u *domain.User) (bool, error)
	DeleteUser(id string) (bool, error)

	// ── Microsoft OAuth state (CSRF) ─────────────────────────────────────────
	SaveOAuthState(state string, expiresAt time.Time) error
	ConsumeOAuthState(state string) (bool, error) // returns true if state was valid & unused

	// ── Access control ───────────────────────────────────────────────────────
	IsEmailAllowed(email string) (bool, error) // domain allowlist OR pre-existing user
	GetAllowedDomains() ([]string, error)
	AddAllowedDomain(domain string) error
	RemoveAllowedDomain(domain string) error

	// ── Categories ───────────────────────────────────────────────────────────
	GetCategories() ([]domain.Category, error)

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

	// ── Bank balance ──────────────────────────────────────────────────────────
	GetBankBalance() (*domain.BankBalance, error)
	SetBankBalance(b domain.BankBalance) error
}
