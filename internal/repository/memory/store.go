// Package memory provides an in-memory Store implementation for local development.
package memory

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
)

// Compile-time check.
var _ repository.Store = (*Store)(nil)

type budgetKey struct {
	category string
	year     int
	month    int
}

type Store struct {
	mu           sync.RWMutex
	transactions map[string]*domain.Transaction
	users        map[string]*domain.User
	domains      map[string]struct{}
	categories   []domain.Category
	settings     map[string]domain.Settings
	activityLogs []domain.ActivityLog
	budgets      map[budgetKey]float64
	siigoConfig  *domain.SiigoConfig
	bankBalance  *domain.BankBalance
}

func New() *Store {
	s := &Store{
		transactions: make(map[string]*domain.Transaction),
		users:        make(map[string]*domain.User),
		domains:      make(map[string]struct{}),
		budgets:      make(map[budgetKey]float64),
		settings:     make(map[string]domain.Settings),
	}
	s.seed()
	return s
}

func (s *Store) seed() {
	now := time.Now()

	s.categories = []domain.Category{
		{ID: uuid.NewString(), Name: "Ventas"},
		{ID: uuid.NewString(), Name: "Servicios"},
		{ID: uuid.NewString(), Name: "Nómina"},
		{ID: uuid.NewString(), Name: "Proveedores"},
		{ID: uuid.NewString(), Name: "Impuestos"},
		{ID: uuid.NewString(), Name: "Arrendamiento"},
		{ID: uuid.NewString(), Name: "Otros"},
	}

	admin := &domain.User{
		ID:        uuid.NewString(),
		Name:      "Admin Local",
		Email:     "admin@arca.local",
		Role:      domain.RoleAdmin,
		Password:  "$2a$10$VsT0saCIhLsPczQPXz4kteeTFi/pD2HiW5xHoOB/VymC6S/Yd6Bau", // "admin"
		Active:    true,
		CreatedAt: now,
	}
	s.users[admin.ID] = admin

	// One year of seed transactions (12 months back from today).
	// Each month gets a fixed set of realistic entries mixing Manual and Siigo sources.
	type txTemplate struct {
		dayOffset   int // day of month (1-based)
		description string
		category    string
		txType      domain.TransactionType
		amount      float64
		status      domain.TransactionStatus
		source      domain.TransactionSource
		externalID  string // non-empty → Siigo
	}

	monthlyTemplates := []txTemplate{
		// Ingresos
		{3, "Venta servicios tecnológicos", "Ventas", domain.TypeIngreso, 18500000, domain.StatusCompleted, domain.SourceSIIGO, "SIIGO-INV-%d%02d-01"},
		{7, "Consultoría empresarial", "Servicios", domain.TypeIngreso, 6200000, domain.StatusCompleted, domain.SourceSIIGO, "SIIGO-INV-%d%02d-02"},
		{12, "Pago cliente Corporación Alfa", "Ventas", domain.TypeIngreso, 9800000, domain.StatusCompleted, domain.SourceManual, ""},
		{18, "Anticipo proyecto Beta", "Ventas", domain.TypeIngreso, 4500000, domain.StatusCompleted, domain.SourceManual, ""},
		{22, "Venta licencias software", "Ventas", domain.TypeIngreso, 3100000, domain.StatusCompleted, domain.SourceSIIGO, "SIIGO-INV-%d%02d-03"},
		{27, "Servicios de soporte mensual", "Servicios", domain.TypeIngreso, 2800000, domain.StatusCompleted, domain.SourceManual, ""},
		// Egresos
		{1, "Nómina quincena 1", "Nómina", domain.TypeEgreso, 8500000, domain.StatusCompleted, domain.SourceManual, ""},
		{5, "Arrendamiento oficina", "Arrendamiento", domain.TypeEgreso, 3200000, domain.StatusCompleted, domain.SourceSIIGO, "SIIGO-EGR-%d%02d-01"},
		{8, "Proveedor insumos TI", "Proveedores", domain.TypeEgreso, 1450000, domain.StatusCompleted, domain.SourceSIIGO, "SIIGO-EGR-%d%02d-02"},
		{10, "Servicios públicos", "Proveedores", domain.TypeEgreso, 580000, domain.StatusCompleted, domain.SourceManual, ""},
		{15, "Nómina quincena 2", "Nómina", domain.TypeEgreso, 8500000, domain.StatusCompleted, domain.SourceManual, ""},
		{16, "IVA declaración bimestral", "Impuestos", domain.TypeEgreso, 2100000, domain.StatusCompleted, domain.SourceSIIGO, "SIIGO-EGR-%d%02d-03"},
		{20, "Mantenimiento equipos", "Proveedores", domain.TypeEgreso, 750000, domain.StatusCompleted, domain.SourceManual, ""},
		{24, "Seguro empresarial", "Otros", domain.TypeEgreso, 490000, domain.StatusCompleted, domain.SourceManual, ""},
		{28, "Papelería y suministros", "Otros", domain.TypeEgreso, 180000, domain.StatusCompleted, domain.SourceManual, ""},
	}

	// Current month gets pending items and a couple of cancelled ones
	currentMonthExtras := []txTemplate{
		{5, "Proyecto Gamma — anticipo", "Ventas", domain.TypeIngreso, 12000000, domain.StatusPending, domain.SourceManual, ""},
		{10, "Renovación plan cloud", "Proveedores", domain.TypeEgreso, 920000, domain.StatusPending, domain.SourceManual, ""},
		{14, "Reintegro viáticos", "Otros", domain.TypeIngreso, 340000, domain.StatusCancelled, domain.SourceManual, ""},
	}

	addTx := func(year, month, day int, tmpl txTemplate) {
		date := time.Date(year, time.Month(month), day, 10, 0, 0, 0, now.Location())
		if date.After(now) {
			date = now
		}
		extID := ""
		if tmpl.externalID != "" {
			extID = fmt.Sprintf(tmpl.externalID, year, month)
		}
		t := &domain.Transaction{
			ID:          uuid.NewString(),
			Date:        date.Format("2006-01-02"),
			Description: tmpl.description,
			Category:    tmpl.category,
			Type:        tmpl.txType,
			Amount:      tmpl.amount,
			Status:      tmpl.status,
			Source:      tmpl.source,
			ExternalID:  extID,
			CreatedAt:   date,
			UpdatedAt:   date,
		}
		s.transactions[t.ID] = t
	}

	// Walk 12 months back (oldest first)
	for monthsBack := 11; monthsBack >= 1; monthsBack-- {
		ref := now.AddDate(0, -monthsBack, 0)
		y, m := ref.Year(), int(ref.Month())
		for _, tmpl := range monthlyTemplates {
			day := tmpl.dayOffset
			addTx(y, m, day, tmpl)
		}
	}

	// Current month: standard templates (completed only) + pending/cancelled extras
	y, m := now.Year(), int(now.Month())
	for _, tmpl := range monthlyTemplates {
		if tmpl.dayOffset > now.Day() {
			continue // don't add future days in current month
		}
		addTx(y, m, tmpl.dayOffset, tmpl)
	}
	for _, tmpl := range currentMonthExtras {
		if tmpl.dayOffset <= now.Day() {
			addTx(y, m, tmpl.dayOffset, tmpl)
		}
	}
}

// ── Transactions ──────────────────────────────────────────────────────────────

func (s *Store) GetAllTransactions() ([]*domain.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*domain.Transaction, 0, len(s.transactions))
	for _, t := range s.transactions {
		cp := *t
		list = append(list, &cp)
	}
	return list, nil
}

func (s *Store) GetTransactionByID(id string) (*domain.Transaction, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.transactions[id]
	if !ok {
		return nil, false, nil
	}
	cp := *t
	return &cp, true, nil
}

func (s *Store) CreateTransaction(t *domain.Transaction) error {
	t.ID = uuid.NewString()
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	s.mu.Lock()
	cp := *t
	s.transactions[t.ID] = &cp
	s.mu.Unlock()
	return nil
}

func (s *Store) ImportTransaction(t *domain.Transaction) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Upsert by ExternalID
	for id, existing := range s.transactions {
		if existing.ExternalID == t.ExternalID {
			existing.Amount = t.Amount
			existing.Status = t.Status
			existing.Description = t.Description
			existing.Date = t.Date
			existing.UpdatedAt = time.Now()
			s.transactions[id] = existing
			return false, nil
		}
	}
	t.ID = uuid.NewString()
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	cp := *t
	s.transactions[t.ID] = &cp
	return true, nil
}

func (s *Store) UpdateTransaction(t *domain.Transaction) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.transactions[t.ID]
	if !ok {
		return false, nil
	}
	t.CreatedAt = existing.CreatedAt
	t.Source = existing.Source
	t.ExternalID = existing.ExternalID
	t.UpdatedAt = time.Now()
	cp := *t
	s.transactions[t.ID] = &cp
	return true, nil
}

func (s *Store) DeleteTransaction(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.transactions[id]
	if !ok {
		return false, nil
	}
	delete(s.transactions, id)
	return true, nil
}


// ── Users ─────────────────────────────────────────────────────────────────────

func (s *Store) GetUserByEmail(email string) (*domain.User, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Active && strings.EqualFold(u.Email, email) {
			cp := *u
			return &cp, true, nil
		}
	}
	return nil, false, nil
}

func (s *Store) GetUserByMicrosoftOID(oid string) (*domain.User, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Active && u.MicrosoftOID == oid {
			cp := *u
			return &cp, true, nil
		}
	}
	return nil, false, nil
}

func (s *Store) GetAllUsers() ([]*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*domain.User, 0, len(s.users))
	for _, u := range s.users {
		cp := *u
		list = append(list, &cp)
	}
	return list, nil
}

func (s *Store) GetUserByID(id string) (*domain.User, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return nil, false, nil
	}
	cp := *u
	return &cp, true, nil
}

func (s *Store) CreateUser(u *domain.User) error {
	u.ID = uuid.NewString()
	u.Active = true
	u.CreatedAt = time.Now()
	s.mu.Lock()
	cp := *u
	s.users[u.ID] = &cp
	s.mu.Unlock()
	return nil
}

func (s *Store) UpdateUser(u *domain.User) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.users[u.ID]
	if !ok {
		return false, nil
	}
	existing.Name = u.Name
	existing.Role = u.Role
	existing.Active = u.Active
	return true, nil
}

func (s *Store) DeleteUser(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return false, nil
	}
	u.Active = false
	return true, nil
}


// ── Access control ────────────────────────────────────────────────────────────

func (s *Store) IsEmailAllowed(email string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Active && strings.EqualFold(u.Email, email) {
			return true, nil
		}
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return false, nil
	}
	_, ok := s.domains[strings.ToLower(parts[1])]
	return ok, nil
}

func (s *Store) GetAllowedDomains() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]string, 0, len(s.domains))
	for d := range s.domains {
		list = append(list, d)
	}
	return list, nil
}

func (s *Store) AddAllowedDomain(d string) error {
	s.mu.Lock()
	s.domains[strings.ToLower(strings.TrimSpace(d))] = struct{}{}
	s.mu.Unlock()
	return nil
}

func (s *Store) RemoveAllowedDomain(d string) error {
	s.mu.Lock()
	delete(s.domains, strings.ToLower(d))
	s.mu.Unlock()
	return nil
}

// ── Categories ────────────────────────────────────────────────────────────────

func (s *Store) GetCategories() ([]domain.Category, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]domain.Category, len(s.categories))
	copy(cp, s.categories)
	return cp, nil
}

// ── Settings ──────────────────────────────────────────────────────────────────

func (s *Store) GetSettings(userID string) (domain.Settings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.settings[userID]; ok {
		return st, nil
	}
	return domain.Settings{BaseCurrency: "COP", AutoExchangeRate: true}, nil
}

func (s *Store) UpdateSettings(userID string, st domain.Settings) error {
	s.mu.Lock()
	s.settings[userID] = st
	s.mu.Unlock()
	return nil
}

// ── Activity logs ─────────────────────────────────────────────────────────────

func (s *Store) GetActivityLogs() ([]domain.ActivityLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.activityLogs)
	if n > 50 {
		n = 50
	}
	cp := make([]domain.ActivityLog, n)
	copy(cp, s.activityLogs[len(s.activityLogs)-n:])
	// reverse so newest first
	for i, j := 0, len(cp)-1; i < j; i, j = i+1, j-1 {
		cp[i], cp[j] = cp[j], cp[i]
	}
	return cp, nil
}

func (s *Store) AddActivityLog(l domain.ActivityLog) error {
	l.ID = uuid.NewString()
	if l.Timestamp.IsZero() {
		l.Timestamp = time.Now()
	}
	s.mu.Lock()
	s.activityLogs = append(s.activityLogs, l)
	s.mu.Unlock()
	return nil
}

// ── Budgets ───────────────────────────────────────────────────────────────────

func (s *Store) GetBudgets() ([]domain.BudgetLine, error) {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	var lines []domain.BudgetLine
	for k, monthly := range s.budgets {
		if k.year == now.Year() && k.month == int(now.Month()) {
			lines = append(lines, domain.BudgetLine{Category: k.category, Monthly: monthly})
		}
	}
	return lines, nil
}

func (s *Store) SetBudgets(budgets []domain.BudgetLine) error {
	now := time.Now()
	s.mu.Lock()
	for _, b := range budgets {
		s.budgets[budgetKey{b.Category, now.Year(), int(now.Month())}] = b.Monthly
	}
	s.mu.Unlock()
	return nil
}

// ── Siigo config ──────────────────────────────────────────────────────────────

func (s *Store) GetSiigoConfig() (*domain.SiigoConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.siigoConfig == nil {
		return nil, nil
	}
	cp := *s.siigoConfig
	return &cp, nil
}

func (s *Store) SetSiigoConfig(cfg domain.SiigoConfig) error {
	s.mu.Lock()
	cp := cfg
	s.siigoConfig = &cp
	s.mu.Unlock()
	return nil
}

func (s *Store) UpdateSiigoLastSync(t time.Time) error {
	s.mu.Lock()
	if s.siigoConfig != nil {
		s.siigoConfig.LastSync = t
	}
	s.mu.Unlock()
	return nil
}

// ── Bank balance ──────────────────────────────────────────────────────────────

func (s *Store) GetBankBalance() (*domain.BankBalance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.bankBalance == nil {
		return nil, nil
	}
	cp := *s.bankBalance
	return &cp, nil
}

func (s *Store) SetBankBalance(b domain.BankBalance) error {
	s.mu.Lock()
	cp := b
	s.bankBalance = &cp
	s.mu.Unlock()
	return nil
}
