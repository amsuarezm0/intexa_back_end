// Package postgres provides a Supabase/PostgreSQL-backed Store implementation.
package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Compile-time check.
var _ repository.Store = (*Store)(nil)

func bg() context.Context { return context.Background() }

// ── Transactions ──────────────────────────────────────────────────────────────

func (s *Store) GetAllTransactions() ([]*domain.Transaction, error) {
	rows, err := s.pool.Query(bg(), `
		SELECT id, date::TEXT, description, category, type, amount, status,
		       COALESCE(reference,''), source, COALESCE(external_id,''), is_projection,
		       created_at, updated_at
		FROM   transactions
		ORDER  BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTransactions(rows)
}

func (s *Store) GetTransactionByID(id string) (*domain.Transaction, bool, error) {
	row := s.pool.QueryRow(bg(), `
		SELECT id, date::TEXT, description, category, type, amount, status,
		       COALESCE(reference,''), source, COALESCE(external_id,''), is_projection,
		       created_at, updated_at
		FROM   transactions WHERE id = $1`, id)
	t, err := scanTransaction(row)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	return t, err == nil, err
}

func (s *Store) CreateTransaction(t *domain.Transaction) error {
	t.ID = uuid.NewString()
	return s.pool.QueryRow(bg(), `
		INSERT INTO transactions
		  (id, date, description, category, type, amount, status, reference, source, external_id, is_projection)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),  $9,NULLIF($10,''),$11)
		RETURNING created_at, updated_at`,
		t.ID, parseDate(t.Date), t.Description, t.Category, string(t.Type),
		t.Amount, string(t.Status), t.Reference, string(t.Source),
		t.ExternalID, t.IsProjection,
	).Scan(&t.CreatedAt, &t.UpdatedAt)
}

// ImportTransaction upserts a Siigo transaction by external_id.
// Returns true if the row was inserted (new), false if it already existed and was updated.
func (s *Store) ImportTransaction(t *domain.Transaction) (bool, error) {
	t.ID = uuid.NewString()
	var inserted bool
	err := s.pool.QueryRow(bg(), `
		INSERT INTO transactions
		  (id, date, description, category, type, amount, status, reference, source, external_id, is_projection)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,NULLIF($10,''),$11)
		ON CONFLICT (external_id) DO UPDATE
		  SET date=$2, description=$3, amount=$6, status=$7, updated_at=now()
		RETURNING (xmax = 0) AS inserted, created_at, updated_at`,
		t.ID, parseDate(t.Date), t.Description, t.Category, string(t.Type),
		t.Amount, string(t.Status), t.Reference, string(t.Source),
		t.ExternalID, t.IsProjection,
	).Scan(&inserted, &t.CreatedAt, &t.UpdatedAt)
	return inserted, err
}

func (s *Store) UpdateTransaction(t *domain.Transaction) (bool, error) {
	tag, err := s.pool.Exec(bg(), `
		UPDATE transactions
		SET    date=$1, description=$2, category=$3, type=$4, amount=$5,
		       status=$6, reference=NULLIF($7,''), source=$8, updated_at=now()
		WHERE  id=$9`,
		parseDate(t.Date), t.Description, t.Category, string(t.Type),
		t.Amount, string(t.Status), t.Reference, string(t.Source), t.ID)
	return tag.RowsAffected() > 0, err
}

func (s *Store) DeleteTransaction(id string) (bool, error) {
	tag, err := s.pool.Exec(bg(), `DELETE FROM transactions WHERE id=$1`, id)
	return tag.RowsAffected() > 0, err
}


// ── Users ─────────────────────────────────────────────────────────────────────

func (s *Store) GetUserByEmail(email string) (*domain.User, bool, error) {
	row := s.pool.QueryRow(bg(), `
		SELECT id, name, email, role, COALESCE(password_hash,''),
		       COALESCE(ms_oid,''), active, last_login_at, created_at
		FROM   users WHERE email=$1 AND active=true`, email)
	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	return u, err == nil, err
}

func (s *Store) GetUserByMicrosoftOID(oid string) (*domain.User, bool, error) {
	row := s.pool.QueryRow(bg(), `
		SELECT id, name, email, role, COALESCE(password_hash,''),
		       COALESCE(ms_oid,''), active, last_login_at, created_at
		FROM   users WHERE ms_oid=$1 AND active=true`, oid)
	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	return u, err == nil, err
}

func (s *Store) GetAllUsers() ([]*domain.User, error) {
	rows, err := s.pool.Query(bg(), `
		SELECT id, name, email, role, COALESCE(password_hash,''),
		       COALESCE(ms_oid,''), active, last_login_at, created_at
		FROM   users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]*domain.User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) GetUserByID(id string) (*domain.User, bool, error) {
	row := s.pool.QueryRow(bg(), `
		SELECT id, name, email, role, COALESCE(password_hash,''),
		       COALESCE(ms_oid,''), active, last_login_at, created_at
		FROM   users WHERE id=$1`, id)
	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	return u, err == nil, err
}

func (s *Store) CreateUser(u *domain.User) error {
	u.ID = uuid.NewString()
	u.Active = true
	return s.pool.QueryRow(bg(), `
		INSERT INTO users (id, name, email, role, password_hash, ms_oid, active)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),true)
		RETURNING created_at`,
		u.ID, u.Name, u.Email, string(u.Role), u.Password, u.MicrosoftOID,
	).Scan(&u.CreatedAt)
}

func (s *Store) UpdateUser(u *domain.User) (bool, error) {
	tag, err := s.pool.Exec(bg(), `
		UPDATE users SET name=$1, role=$2, active=$3, updated_at=now()
		WHERE  id=$4`,
		u.Name, string(u.Role), u.Active, u.ID)
	return tag.RowsAffected() > 0, err
}

func (s *Store) DeleteUser(id string) (bool, error) {
	// Soft-delete: mark inactive
	tag, err := s.pool.Exec(bg(),
		`UPDATE users SET active=false, updated_at=now() WHERE id=$1`, id)
	return tag.RowsAffected() > 0, err
}

// ── Access control ────────────────────────────────────────────────────────────

func (s *Store) IsEmailAllowed(email string) (bool, error) {
	// Existing active user is always allowed
	var exists bool
	if err := s.pool.QueryRow(bg(),
		`SELECT EXISTS(SELECT 1 FROM users WHERE email=$1 AND active=true)`, email,
	).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return true, nil
	}
	// Check domain allowlist
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return false, nil
	}
	if err := s.pool.QueryRow(bg(),
		`SELECT EXISTS(SELECT 1 FROM allowed_domains WHERE lower(domain)=lower($1))`,
		parts[1],
	).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *Store) GetAllowedDomains() ([]string, error) {
	rows, err := s.pool.Query(bg(), `SELECT domain FROM allowed_domains ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var domains []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	return domains, rows.Err()
}

func (s *Store) AddAllowedDomain(domain string) error {
	_, err := s.pool.Exec(bg(),
		`INSERT INTO allowed_domains (domain) VALUES (lower($1)) ON CONFLICT (domain) DO NOTHING`,
		strings.TrimSpace(domain))
	return err
}

func (s *Store) RemoveAllowedDomain(domain string) error {
	_, err := s.pool.Exec(bg(),
		`DELETE FROM allowed_domains WHERE lower(domain)=lower($1)`, domain)
	return err
}

// ── Categories ────────────────────────────────────────────────────────────────

func (s *Store) GetCategories() ([]domain.Category, error) {
	rows, err := s.pool.Query(bg(),
		`SELECT id::TEXT, name FROM categories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cats := make([]domain.Category, 0)
	for rows.Next() {
		var c domain.Category
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

// ── Settings ──────────────────────────────────────────────────────────────────

func (s *Store) GetSettings(userID string) (domain.Settings, error) {
	st := domain.Settings{BaseCurrency: "COP", AutoExchangeRate: true}
	rows, err := s.pool.Query(bg(), `
		SELECT key, value FROM settings WHERE user_id=$1`, userID)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	kv := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, &v); err != nil {
			return st, err
		}
		kv[k] = v
	}
	if v, ok := kv["baseCurrency"]; ok {
		json.Unmarshal(v, &st.BaseCurrency)
	}
	if v, ok := kv["autoExchangeRate"]; ok {
		json.Unmarshal(v, &st.AutoExchangeRate)
	}
	return st, rows.Err()
}

func (s *Store) UpdateSettings(userID string, st domain.Settings) error {
	_, err := s.pool.Exec(bg(), `
		INSERT INTO settings (user_id, key, value) VALUES
		  ($1, 'baseCurrency',     to_jsonb($2::text)),
		  ($1, 'autoExchangeRate', to_jsonb($3::bool))
		ON CONFLICT (user_id, key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`,
		userID, st.BaseCurrency, st.AutoExchangeRate)
	return err
}

// ── Activity logs ─────────────────────────────────────────────────────────────

func (s *Store) GetActivityLogs() ([]domain.ActivityLog, error) {
	rows, err := s.pool.Query(bg(), `
		SELECT id::TEXT, user_name, initial, action, module,
		       COALESCE(color,''), created_at
		FROM   activity_logs
		ORDER  BY created_at DESC
		LIMIT  50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]domain.ActivityLog, 0)
	for rows.Next() {
		var l domain.ActivityLog
		if err := rows.Scan(&l.ID, &l.UserName, &l.Initial, &l.Action,
			&l.Module, &l.Color, &l.Timestamp); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func (s *Store) AddActivityLog(l domain.ActivityLog) error {
	_, err := s.pool.Exec(bg(), `
		INSERT INTO activity_logs (user_name, initial, action, module, color)
		VALUES ($1,$2,$3,$4,NULLIF($5,''))`,
		l.UserName, l.Initial, l.Action, l.Module, l.Color)
	return err
}

// ── Budgets ───────────────────────────────────────────────────────────────────

func (s *Store) GetBudgets() ([]domain.BudgetLine, error) {
	now := time.Now()
	rows, err := s.pool.Query(bg(), `
		SELECT category, monthly FROM budget_lines
		WHERE  year=$1 AND month=$2
		ORDER  BY category`,
		now.Year(), int(now.Month()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lines []domain.BudgetLine
	for rows.Next() {
		var b domain.BudgetLine
		if err := rows.Scan(&b.Category, &b.Monthly); err != nil {
			return nil, err
		}
		lines = append(lines, b)
	}
	return lines, rows.Err()
}

func (s *Store) SetBudgets(budgets []domain.BudgetLine) error {
	now := time.Now()
	for _, b := range budgets {
		if _, err := s.pool.Exec(bg(), `
			INSERT INTO budget_lines (category, monthly, year, month)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (category, year, month)
			DO UPDATE SET monthly=EXCLUDED.monthly, updated_at=now()`,
			b.Category, b.Monthly, now.Year(), int(now.Month())); err != nil {
			return err
		}
	}
	return nil
}

// ── Siigo config ──────────────────────────────────────────────────────────────

func (s *Store) GetSiigoConfig() (*domain.SiigoConfig, error) {
	var cfg domain.SiigoConfig
	var tokenExp *time.Time
	var lastSync *time.Time
	err := s.pool.QueryRow(bg(), `
		SELECT user_name, COALESCE(access_key_enc,''), partner_id, token_expires_at, last_sync_at
		FROM   siigo_configs ORDER BY created_at DESC LIMIT 1`,
	).Scan(&cfg.UserName, &cfg.AccessKey, &cfg.PartnerID, &tokenExp, &lastSync)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg.Connected = tokenExp != nil && time.Now().Before(*tokenExp)
	if tokenExp != nil {
		cfg.TokenExp = *tokenExp
	}
	if lastSync != nil {
		cfg.LastSync = *lastSync
	}
	return &cfg, nil
}

func (s *Store) SetSiigoConfig(cfg domain.SiigoConfig) error {
	tag, err := s.pool.Exec(bg(), `
		UPDATE siigo_configs
		SET user_name=$1, access_key_enc=$2, partner_id=COALESCE($3,''), token_expires_at=$4, updated_at=now()
		WHERE id = (SELECT id FROM siigo_configs ORDER BY created_at DESC LIMIT 1)`,
		cfg.UserName, cfg.AccessKey, cfg.PartnerID, cfg.TokenExp)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = s.pool.Exec(bg(), `
		INSERT INTO siigo_configs (user_name, access_key_enc, partner_id, token_expires_at)
		VALUES ($1, $2, COALESCE($3,''), $4)`,
		cfg.UserName, cfg.AccessKey, cfg.PartnerID, cfg.TokenExp)
	return err
}

func (s *Store) UpdateSiigoLastSync(t time.Time) error {
	_, err := s.pool.Exec(bg(), `
		UPDATE siigo_configs SET last_sync_at=$1, updated_at=now()
		WHERE  id = (SELECT id FROM siigo_configs ORDER BY created_at DESC LIMIT 1)`, t)
	return err
}

// ── Bank balance ──────────────────────────────────────────────────────────────

func (s *Store) GetBankBalance() (*domain.BankBalance, error) {
	var b domain.BankBalance
	err := s.pool.QueryRow(bg(), `
		SELECT amount, updated_by, updated_at FROM bank_balance ORDER BY id LIMIT 1`,
	).Scan(&b.Amount, &b.UpdatedBy, &b.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

func (s *Store) SetBankBalance(b domain.BankBalance) error {
	_, err := s.pool.Exec(bg(), `
		INSERT INTO bank_balance (amount, updated_by, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (id) DO UPDATE
			SET amount=$1, updated_by=$2, updated_at=now()`,
		b.Amount, b.UpdatedBy)
	return err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanTransaction(row scanner) (*domain.Transaction, error) {
	var t domain.Transaction
	var txType, txStatus, txSource string
	err := row.Scan(&t.ID, &t.Date, &t.Description, &t.Category,
		&txType, &t.Amount, &txStatus, &t.Reference,
		&txSource, &t.ExternalID, &t.IsProjection,
		&t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.Type = domain.TransactionType(txType)
	t.Status = domain.TransactionStatus(txStatus)
	t.Source = domain.TransactionSource(txSource)
	return &t, nil
}

func scanTransactions(rows pgx.Rows) ([]*domain.Transaction, error) {
	list := make([]*domain.Transaction, 0)
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

func scanUser(row scanner) (*domain.User, error) {
	var u domain.User
	var role string
	err := row.Scan(&u.ID, &u.Name, &u.Email, &role, &u.Password,
		&u.MicrosoftOID, &u.Active, &u.LastLoginAt, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Role = domain.UserRole(role)
	return &u, nil
}

// parseDate parses human-readable dates ("02 Jan, 2006") or ISO dates ("2006-01-02").
func parseDate(s string) string {
	for _, layout := range []string{"02 Jan, 2006", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return time.Now().Format("2006-01-02")
}
