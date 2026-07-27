// Package memory provides an in-memory Store implementation for local development.
package memory

import (
	"fmt"
	"sort"
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
	invoices     map[string]*domain.Invoice
	purchases    map[string]*domain.Purchase
	users        map[string]*domain.User
	domains      map[string]struct{}
	categories   []domain.Category
	settings     map[string]domain.Settings
	activityLogs []domain.ActivityLog
	budgets      map[budgetKey]float64
	siigoConfig  *domain.SiigoConfig
	bankBalance  *domain.BankBalance
	projPeriods  []domain.ProjectionPeriod
}

func New() *Store {
	s := &Store{
		transactions: make(map[string]*domain.Transaction),
		invoices:     make(map[string]*domain.Invoice),
		purchases:    make(map[string]*domain.Purchase),
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
		{ID: uuid.NewString(), Name: "Operacional - Ventas"},
		{ID: uuid.NewString(), Name: "Ingresos Editoriales"},
		{ID: uuid.NewString(), Name: "Ingresos Directos"},
		{ID: uuid.NewString(), Name: "Finanzas - Inversiones"},
		{ID: uuid.NewString(), Name: "Gastos - Personal"},
		{ID: uuid.NewString(), Name: "Gastos - Tecnología"},
		{ID: uuid.NewString(), Name: "Gastos Operativos"},
		{ID: uuid.NewString(), Name: "Marketing"},
		{ID: uuid.NewString(), Name: "Infraestructura"},
	}

	admin := &domain.User{
		ID:        uuid.NewString(),
		Name:      "Admin Local",
		Email:     "admin@arca.local",
		Role:      domain.RoleAdmin,
		Password:  "$2a$10$M7LTaakjAUg8F9D02Uv5su3eggah2nA4fa.B6TDjKPq8bvilBN9W6", // bcrypt of sha256("admin") — the frontend SHA-256s the password client-side before sending
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
		reference := ""
		if tmpl.externalID != "" {
			extID = fmt.Sprintf(tmpl.externalID, year, month)
			// Siigo cash movements carry an RC (Recibo de Caja, income) or
			// RP (Recibo de Pago, expense) reference, reusing the external id's
			// trailing sequence for uniqueness.
			docPrefix := "RC"
			if tmpl.txType == domain.TypeEgreso {
				docPrefix = "RP"
			}
			reference = fmt.Sprintf("%s-%d%02d-%s", docPrefix, year, month, extID[strings.LastIndex(extID, "-")+1:])
		}
		t := &domain.Transaction{
			ID:          uuid.NewString(),
			Date:        date.Format("2006-01-02"),
			Description: tmpl.description,
			Category:    tmpl.category,
			Type:        tmpl.txType,
			Amount:      tmpl.amount,
			Status:      tmpl.status,
			Reference:   reference,
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

	// ── Siigo documents: invoices (FV) and purchases (FC) ────────────────────
	// Receivables/payables (which can be Parcial/Pendiente), kept separate from
	// the cash movements above and searchable by their FV-/FC- reference.
	type docTemplate struct {
		seq          int
		description  string
		category     string
		counterparty string
		counterID    string
		total        float64
		balance      float64
		status       domain.TransactionStatus
		installments int // 0/1 → lump sum; N → monthly schedule from due date
	}
	invoiceTemplates := []docTemplate{
		{1, "Factura servicios editoriales", "Operacional - Ventas", "Corporación Alfa S.A.S.", "900123456", 15400000, 0, domain.StatusCompleted, 0},
		{2, "Factura licenciamiento anual", "Ingresos Editoriales", "Editorial Beta Ltda.", "830987654", 8900000, 4450000, domain.StatusPartial, 0},
		{3, "Factura consultoría trimestral (3 cuotas)", "Ingresos Directos", "Gamma Consulting", "901222333", 6200000, 6200000, domain.StatusPending, 3},
	}
	purchaseTemplates := []docTemplate{
		{1, "Compra equipos de cómputo", "Gastos - Tecnología", "TechSupply S.A.", "860111222", 7300000, 0, domain.StatusCompleted, 0},
		{2, "Servicios de nube — plan a cuotas", "Infraestructura", "CloudCo Colombia", "901555666", 4200000, 2100000, domain.StatusPartial, 3},
		{3, "Suministros de oficina", "Gastos Operativos", "Papelería Central", "800333444", 950000, 950000, domain.StatusPending, 0},
	}

	// mkSchedule builds an N-installment monthly schedule starting at `first`,
	// each installment total/N (remainder on the last). N<=1 → lump sum (nil).
	mkSchedule := func(total float64, n int, first time.Time) []domain.Installment {
		if n <= 1 {
			return nil
		}
		out := make([]domain.Installment, 0, n)
		each := total / float64(n)
		acc := 0.0
		for k := 0; k < n; k++ {
			v := each
			if k == n-1 {
				v = total - acc
			}
			acc += each
			out = append(out, domain.Installment{DueDate: first.AddDate(0, k, 0).Format("2006-01-02"), Value: v})
		}
		return out
	}

	for monthsBack := 2; monthsBack >= 0; monthsBack-- {
		ref := now.AddDate(0, -monthsBack, 0)
		yy, mm := ref.Year(), int(ref.Month())
		issue := time.Date(yy, time.Month(mm), 5, 10, 0, 0, 0, now.Location())
		if issue.After(now) {
			issue = now
		}
		due := issue.AddDate(0, 1, 0)
		for _, d := range invoiceTemplates {
			inv := &domain.Invoice{
				ID:                     uuid.NewString(),
				ExternalID:             fmt.Sprintf("siigo-fv-%d%02d-%02d", yy, mm, d.seq),
				Source:                 string(domain.SourceSIIGO),
				Reference:              fmt.Sprintf("FV-%d%02d-%02d", yy, mm, d.seq),
				Prefix:                 "FV",
				Number:                 d.seq,
				Date:                   issue.Format("2006-01-02"),
				DueDate:                due.Format("2006-01-02"),
				CustomerIdentification: d.counterID,
				CustomerName:           d.counterparty,
				Total:                  d.total,
				Balance:                d.balance,
				Status:                 d.status,
				Category:               d.category,
				Detail:                 d.description,
				Installments:           mkSchedule(d.total, d.installments, due),
				SyncedAt:               issue,
				CreatedAt:              issue,
				UpdatedAt:              issue,
			}
			s.invoices[inv.ID] = inv
		}
		for _, d := range purchaseTemplates {
			pur := &domain.Purchase{
				ID:                     uuid.NewString(),
				ExternalID:             fmt.Sprintf("siigo-fc-%d%02d-%02d", yy, mm, d.seq),
				Source:                 string(domain.SourceSIIGO),
				Reference:              fmt.Sprintf("FC-%d%02d-%02d", yy, mm, d.seq),
				Prefix:                 "FC",
				Number:                 d.seq,
				Date:                   issue.Format("2006-01-02"),
				DueDate:                due.Format("2006-01-02"),
				ProviderIdentification: d.counterID,
				ProviderName:           d.counterparty,
				Total:                  d.total,
				Balance:                d.balance,
				Status:                 d.status,
				Category:               d.category,
				Detail:                 d.description,
				Installments:           mkSchedule(d.total, d.installments, due),
				SyncedAt:               issue,
				CreatedAt:              issue,
				UpdatedAt:              issue,
			}
			s.purchases[pur.ID] = pur
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

func (s *Store) GetProjectionPeriods() ([]domain.ProjectionPeriod, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.ProjectionPeriod, len(s.projPeriods))
	copy(out, s.projPeriods)
	sort.Slice(out, func(i, j int) bool { return out[i].Days < out[j].Days })
	return out, nil
}

func (s *Store) CreateProjectionPeriod(p *domain.ProjectionPeriod) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.projPeriods {
		if existing.Days == p.Days {
			return fmt.Errorf("projection period with %d days already exists", p.Days)
		}
	}
	p.ID = uuid.NewString()
	p.CreatedAt = time.Now()
	s.projPeriods = append(s.projPeriods, *p)
	return nil
}

func (s *Store) DeleteProjectionPeriod(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.projPeriods {
		if p.ID == id {
			s.projPeriods = append(s.projPeriods[:i], s.projPeriods[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) NextManualReference(prefix string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	max := 0
	for _, t := range s.transactions {
		var n int
		if _, err := fmt.Sscanf(t.Reference, prefix+"-%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("%s-%06d", prefix, max+1), nil
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
			t.ID = id
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

// ── Focused aggregation queries ───────────────────────────────────────────────

func (s *Store) GetCurrentBalance() (float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var bal float64
	for _, t := range s.transactions {
		if t.IsProjection || t.Status != domain.StatusCompleted {
			continue
		}
		if t.Type == domain.TypeIngreso {
			bal += t.Amount
		} else {
			bal -= t.Amount
		}
	}
	return bal, nil
}

func (s *Store) GetMonthlyTotals(from, to time.Time) ([]domain.MonthlyTotal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	type key struct{ y, m int }
	agg := map[key]*domain.MonthlyTotal{}
	for _, t := range s.transactions {
		if t.IsProjection || t.Status != domain.StatusCompleted {
			continue
		}
		d, err := time.ParseInLocation("2006-01-02", t.Date, time.Local)
		if err != nil {
			continue
		}
		if d.Before(from) || d.After(to) {
			continue
		}
		k := key{d.Year(), int(d.Month())}
		if agg[k] == nil {
			agg[k] = &domain.MonthlyTotal{Year: d.Year(), Month: d.Month()}
		}
		if t.Type == domain.TypeIngreso {
			agg[k].Income += t.Amount
		} else {
			agg[k].Expense += t.Amount
		}
	}
	out := make([]domain.MonthlyTotal, 0, len(agg))
	for _, v := range agg {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Year != out[j].Year {
			return out[i].Year < out[j].Year
		}
		return out[i].Month < out[j].Month
	})
	return out, nil
}

func (s *Store) GetDailyTotals(from, to time.Time) ([]domain.DailyTotal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agg := map[string]*domain.DailyTotal{}
	for _, t := range s.transactions {
		if t.IsProjection || t.Status != domain.StatusCompleted {
			continue
		}
		d, err := time.ParseInLocation("2006-01-02", t.Date, time.Local)
		if err != nil {
			continue
		}
		if d.Before(from) || d.After(to) {
			continue
		}
		if agg[t.Date] == nil {
			agg[t.Date] = &domain.DailyTotal{Date: t.Date}
		}
		if t.Type == domain.TypeIngreso {
			agg[t.Date].Ingresos += t.Amount
		} else {
			agg[t.Date].Egresos += t.Amount
		}
	}
	out := make([]domain.DailyTotal, 0, len(agg))
	for _, v := range agg {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

func (s *Store) GetPendingTransactions() ([]*domain.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Transaction
	for _, t := range s.transactions {
		if t.Status == domain.StatusPending && !t.IsProjection {
			cp := *t
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

func (s *Store) GetPendingProjections(horizon time.Time) ([]*domain.Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Transaction
	for _, t := range s.transactions {
		if !t.IsProjection || t.Status == domain.StatusCancelled {
			continue
		}
		d, err := time.ParseInLocation("2006-01-02", t.Date, time.Local)
		if err != nil {
			continue
		}
		if !d.After(horizon) {
			cp := *t
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

func (s *Store) GetCategoryTotals(from, to time.Time, txType domain.TransactionType) ([]domain.CategoryTotal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agg := map[string]float64{}
	for _, t := range s.transactions {
		if t.IsProjection || t.Status != domain.StatusCompleted || t.Type != txType {
			continue
		}
		d, err := time.ParseInLocation("2006-01-02", t.Date, time.Local)
		if err != nil {
			continue
		}
		if d.Before(from) || d.After(to) {
			continue
		}
		agg[t.Category] += t.Amount
	}
	out := make([]domain.CategoryTotal, 0, len(agg))
	for cat, amt := range agg {
		out = append(out, domain.CategoryTotal{Category: cat, Amount: amt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Amount > out[j].Amount })
	return out, nil
}

func (s *Store) GetWeeklyTotals(year int, month time.Month) ([]domain.WeeklyComparison, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agg := map[int]*domain.WeeklyComparison{}
	for _, t := range s.transactions {
		if t.IsProjection || t.Status != domain.StatusCompleted {
			continue
		}
		d, err := time.ParseInLocation("2006-01-02", t.Date, time.Local)
		if err != nil {
			continue
		}
		if d.Year() != year || d.Month() != month {
			continue
		}
		week := (d.Day()-1)/7 + 1
		if agg[week] == nil {
			agg[week] = &domain.WeeklyComparison{Week: week}
		}
		if t.Type == domain.TypeIngreso {
			agg[week].Ingresos += t.Amount
		} else {
			agg[week].Egresos += t.Amount
		}
	}
	out := make([]domain.WeeklyComparison, 0, len(agg))
	for _, v := range agg {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Week < out[j].Week })
	return out, nil
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
	s.settings[u.ID] = domain.Settings{BaseCurrency: "COP", AutoExchangeRate: true, Theme: "predeterminado"}
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

func (s *Store) UpdatePassword(userID, hashedPassword string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return false, nil
	}
	u.Password = hashedPassword
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
	return domain.Settings{BaseCurrency: "COP", AutoExchangeRate: true, Theme: "predeterminado"}, nil
}

func (s *Store) UpdateSettings(userID string, st domain.Settings) error {
	if st.Theme == "" {
		st.Theme = "predeterminado"
	}
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

func (s *Store) GetEarliestSiigoDate() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	earliest := ""
	updateEarliest := func(date string) {
		if date != "" && (earliest == "" || date < earliest) {
			earliest = date
		}
	}
	for _, t := range s.transactions {
		if t.Source == domain.SourceSIIGO {
			updateEarliest(t.Date)
		}
	}
	for _, inv := range s.invoices {
		updateEarliest(inv.Date)
	}
	for _, pur := range s.purchases {
		updateEarliest(pur.Date)
	}
	return earliest, nil
}

func (s *Store) GetOldestPendingOrPartialDate() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	oldest := ""
	updateOldest := func(date string) {
		if date != "" && (oldest == "" || date < oldest) {
			oldest = date
		}
	}
	for _, inv := range s.invoices {
		if inv.Status == domain.StatusPending || inv.Status == domain.StatusPartial {
			updateOldest(inv.Date)
		}
	}
	for _, pur := range s.purchases {
		if pur.Status == domain.StatusPending || pur.Status == domain.StatusPartial {
			updateOldest(pur.Date)
		}
	}
	return oldest, nil
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

// ── Invoices ──────────────────────────────────────────────────────────────────

func (s *Store) GetAllInvoices() ([]*domain.Invoice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*domain.Invoice, 0, len(s.invoices))
	for _, inv := range s.invoices {
		cp := *inv
		list = append(list, &cp)
	}
	return list, nil
}

func (s *Store) GetPendingInvoices() ([]*domain.Invoice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*domain.Invoice, 0)
	for _, inv := range s.invoices {
		if inv.Status == domain.StatusPending || inv.Status == domain.StatusPartial {
			cp := *inv
			list = append(list, &cp)
		}
	}
	return list, nil
}

func (s *Store) GetInvoiceByID(id string) (*domain.Invoice, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inv, ok := s.invoices[id]
	if !ok {
		return nil, false, nil
	}
	cp := *inv
	return &cp, true, nil
}

func (s *Store) UpsertInvoice(inv *domain.Invoice) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.invoices {
		if existing.ExternalID == inv.ExternalID {
			existing.Date = inv.Date
			existing.DueDate = inv.DueDate
			existing.CustomerIdentification = inv.CustomerIdentification
			existing.CustomerName = inv.CustomerName
			existing.Total = inv.Total
			existing.Balance = inv.Balance
			existing.Status = inv.Status
			existing.Category = inv.Category
			existing.Detail = inv.Detail
			existing.SyncedAt = time.Now()
			existing.UpdatedAt = time.Now()
			s.invoices[id] = existing
			inv.ID = id
			return false, nil
		}
	}
	inv.ID = uuid.NewString()
	now := time.Now()
	inv.SyncedAt = now
	inv.CreatedAt = now
	inv.UpdatedAt = now
	cp := *inv
	s.invoices[inv.ID] = &cp
	return true, nil
}

// ── Purchases ─────────────────────────────────────────────────────────────────

func (s *Store) GetAllPurchases() ([]*domain.Purchase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*domain.Purchase, 0, len(s.purchases))
	for _, pur := range s.purchases {
		cp := *pur
		list = append(list, &cp)
	}
	return list, nil
}

func (s *Store) GetPendingPurchases() ([]*domain.Purchase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*domain.Purchase, 0)
	for _, pur := range s.purchases {
		if pur.Status == domain.StatusPending || pur.Status == domain.StatusPartial {
			cp := *pur
			list = append(list, &cp)
		}
	}
	return list, nil
}

func (s *Store) GetPurchaseByID(id string) (*domain.Purchase, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pur, ok := s.purchases[id]
	if !ok {
		return nil, false, nil
	}
	cp := *pur
	return &cp, true, nil
}

func (s *Store) UpsertPurchase(pur *domain.Purchase) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.purchases {
		if existing.ExternalID == pur.ExternalID {
			existing.Date = pur.Date
			existing.DueDate = pur.DueDate
			existing.ProviderIdentification = pur.ProviderIdentification
			existing.ProviderName = pur.ProviderName
			existing.Total = pur.Total
			existing.Balance = pur.Balance
			existing.Status = pur.Status
			existing.Category = pur.Category
			existing.Detail = pur.Detail
			existing.SyncedAt = time.Now()
			existing.UpdatedAt = time.Now()
			s.purchases[id] = existing
			pur.ID = id
			return false, nil
		}
	}
	pur.ID = uuid.NewString()
	now := time.Now()
	pur.SyncedAt = now
	pur.CreatedAt = now
	pur.UpdatedAt = now
	cp := *pur
	s.purchases[pur.ID] = &cp
	return true, nil
}

func (s *Store) GetPeriodData(from, to time.Time) (*domain.PeriodData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fromStr := from.Format("2006-01-02")
	toStr := to.Format("2006-01-02")
	inRange := func(d string) bool { return d != "" && d >= fromStr && d <= toStr }

	txs := make([]*domain.Transaction, 0)
	for _, t := range s.transactions {
		if inRange(t.Date) {
			cp := *t
			txs = append(txs, &cp)
		}
	}

	// A pending doc belongs to the period if its due date or any installment
	// falls within it.
	invs := make([]*domain.Invoice, 0)
	for _, inv := range s.invoices {
		if inv.Status != domain.StatusPending && inv.Status != domain.StatusPartial {
			continue
		}
		hit := inRange(firstNonEmpty(inv.DueDate, inv.Date))
		for _, ins := range inv.Installments {
			if inRange(ins.DueDate) {
				hit = true
				break
			}
		}
		if hit {
			cp := *inv
			invs = append(invs, &cp)
		}
	}

	purs := make([]*domain.Purchase, 0)
	for _, pur := range s.purchases {
		if pur.Status != domain.StatusPending && pur.Status != domain.StatusPartial {
			continue
		}
		hit := inRange(firstNonEmpty(pur.DueDate, pur.Date))
		for _, ins := range pur.Installments {
			if inRange(ins.DueDate) {
				hit = true
				break
			}
		}
		if hit {
			cp := *pur
			purs = append(purs, &cp)
		}
	}

	return &domain.PeriodData{Transactions: txs, Invoices: invs, Purchases: purs}, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Store) Search(reference string) ([]domain.SearchDocument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(reference))
	if q == "" {
		return []domain.SearchDocument{}, nil
	}
	prefix := strings.ToUpper(q)
	if len(prefix) >= 2 {
		prefix = prefix[:2]
	}

	out := make([]domain.SearchDocument, 0)
	switch prefix {
	case "FV":
		for _, inv := range s.invoices {
			if strings.Contains(strings.ToLower(inv.Reference), q) {
				out = append(out, invoiceDoc(inv))
			}
		}
	case "FC":
		for _, pur := range s.purchases {
			if strings.Contains(strings.ToLower(pur.Reference), q) {
				out = append(out, purchaseDoc(pur))
			}
		}
	default:
		// RC/RP live in transactions; anything else falls back to a plain match.
		docType := prefix
		if prefix != "RC" && prefix != "RP" {
			docType = "TX"
		}
		for _, t := range s.transactions {
			if strings.Contains(strings.ToLower(t.Reference), q) {
				out = append(out, transactionDoc(t, docType))
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	if len(out) > 50 {
		out = out[:50]
	}
	return out, nil
}

const tsLayout = "2006-01-02 15:04:05"

func transactionDoc(t *domain.Transaction, docType string) domain.SearchDocument {
	return domain.SearchDocument{
		ID:           t.ID,
		DocType:      docType,
		Reference:    t.Reference,
		Date:         t.Date,
		Description:  t.Description,
		Detail:       t.Detail,
		Category:     t.Category,
		Type:         string(t.Type),
		Amount:       t.Amount,
		Status:       string(t.Status),
		Source:       string(t.Source),
		ExternalID:   t.ExternalID,
		IsProjection: t.IsProjection,
		CreatedAt:    t.CreatedAt.Format(tsLayout),
		UpdatedAt:    t.UpdatedAt.Format(tsLayout),
	}
}

func invoiceDoc(inv *domain.Invoice) domain.SearchDocument {
	return domain.SearchDocument{
		ID:             inv.ID,
		DocType:        "FV",
		Reference:      inv.Reference,
		Date:           inv.Date,
		DueDate:        inv.DueDate,
		Description:    inv.Detail,
		Category:       inv.Category,
		Amount:         inv.Total,
		Balance:        inv.Balance,
		Status:         string(inv.Status),
		Counterparty:   inv.CustomerName,
		CounterpartyID: inv.CustomerIdentification,
		Source:         inv.Source,
		Prefix:         inv.Prefix,
		Number:         inv.Number,
		IsProjection:   inv.IsProjection,
		ExternalID:     inv.ExternalID,
		SyncedAt:       inv.SyncedAt.Format(tsLayout),
		CreatedAt:      inv.CreatedAt.Format(tsLayout),
		UpdatedAt:      inv.UpdatedAt.Format(tsLayout),
	}
}

func purchaseDoc(pur *domain.Purchase) domain.SearchDocument {
	return domain.SearchDocument{
		ID:             pur.ID,
		DocType:        "FC",
		Reference:      pur.Reference,
		Date:           pur.Date,
		DueDate:        pur.DueDate,
		Description:    pur.Detail,
		Category:       pur.Category,
		Amount:         pur.Total,
		Balance:        pur.Balance,
		Status:         string(pur.Status),
		Counterparty:   pur.ProviderName,
		CounterpartyID: pur.ProviderIdentification,
		Source:         pur.Source,
		Prefix:         pur.Prefix,
		Number:         pur.Number,
		IsProjection:   pur.IsProjection,
		ExternalID:     pur.ExternalID,
		SyncedAt:       pur.SyncedAt.Format(tsLayout),
		CreatedAt:      pur.CreatedAt.Format(tsLayout),
		UpdatedAt:      pur.UpdatedAt.Format(tsLayout),
	}
}
