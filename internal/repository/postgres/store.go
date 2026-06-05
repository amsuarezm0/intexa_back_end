// Package postgres provides a Supabase/PostgreSQL-backed Store implementation.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
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
		       COALESCE(reference,''), COALESCE(detail,''), source, COALESCE(external_id,''),
		       is_projection, created_at, updated_at
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
		       COALESCE(reference,''), COALESCE(detail,''), source, COALESCE(external_id,''),
		       is_projection, created_at, updated_at
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
		  (id, date, description, category, type, amount, status, reference, detail, source, external_id, is_projection)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,NULLIF($11,''),$12)
		RETURNING created_at, updated_at`,
		t.ID, parseDate(t.Date), t.Description, t.Category, string(t.Type),
		t.Amount, string(t.Status), t.Reference, t.Detail, string(t.Source),
		t.ExternalID, t.IsProjection,
	).Scan(&t.CreatedAt, &t.UpdatedAt)
}

// ImportTransaction upserts a Siigo transaction (RC/RP) by external_id.
// Returns true if the row was inserted (new), false if updated.
// After the call, t.ID is set to the actual row ID.
func (s *Store) ImportTransaction(t *domain.Transaction) (bool, error) {
	newID := uuid.NewString()
	var actualID string
	var inserted bool
	err := s.pool.QueryRow(bg(), `
		INSERT INTO transactions
		  (id, date, description, category, type, amount, status, reference, detail, source, external_id, is_projection)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,NULLIF($11,''),$12)
		ON CONFLICT (external_id) DO UPDATE
		  SET date=$2, description=$3, category=$4, amount=$6, status=$7, reference=NULLIF($8,''), detail=$9, updated_at=now()
		RETURNING id, (xmax = 0) AS inserted, created_at, updated_at`,
		newID, parseDate(t.Date), t.Description, t.Category, string(t.Type),
		t.Amount, string(t.Status), t.Reference, t.Detail, string(t.Source),
		t.ExternalID, t.IsProjection,
	).Scan(&actualID, &inserted, &t.CreatedAt, &t.UpdatedAt)
	t.ID = actualID
	return inserted, err
}

func (s *Store) UpdateTransaction(t *domain.Transaction) (bool, error) {
	tag, err := s.pool.Exec(bg(), `
		UPDATE transactions
		SET    date=$1, description=$2, category=$3, type=$4, amount=$5,
		       status=$6, reference=NULLIF($7,''), detail=$8, source=$9, updated_at=now()
		WHERE  id=$10`,
		parseDate(t.Date), t.Description, t.Category, string(t.Type),
		t.Amount, string(t.Status), t.Reference, t.Detail, string(t.Source), t.ID)
	return tag.RowsAffected() > 0, err
}

func (s *Store) DeleteTransaction(id string) (bool, error) {
	tag, err := s.pool.Exec(bg(), `DELETE FROM transactions WHERE id=$1`, id)
	return tag.RowsAffected() > 0, err
}

// ── Focused aggregation queries ───────────────────────────────────────────────

func (s *Store) GetCurrentBalance() (float64, error) {
	var bal float64
	err := s.pool.QueryRow(bg(), `
		SELECT COALESCE(SUM(net), 0) FROM (
			SELECT CASE WHEN type='Ingreso' THEN amount ELSE -amount END AS net
			FROM   transactions
			WHERE  status='Completado' AND is_projection=false
			UNION ALL
			SELECT amount AS net FROM invoices  WHERE status='Completado'
			UNION ALL
			SELECT -amount AS net FROM purchases WHERE status='Completado'
		) sub`).Scan(&bal)
	return bal, err
}

func (s *Store) GetMonthlyTotals(from, to time.Time) ([]domain.MonthlyTotal, error) {
	rows, err := s.pool.Query(bg(), `
		SELECT EXTRACT(YEAR FROM date)::int,
		       EXTRACT(MONTH FROM date)::int,
		       COALESCE(SUM(income), 0),
		       COALESCE(SUM(expense), 0)
		FROM (
			SELECT date, amount AS income, 0 AS expense FROM transactions
			WHERE  status='Completado' AND is_projection=false AND type='Ingreso'
			  AND  date >= $1 AND date <= $2
			UNION ALL
			SELECT date, 0 AS income, amount AS expense FROM transactions
			WHERE  status='Completado' AND is_projection=false AND type='Egreso'
			  AND  date >= $1 AND date <= $2
			UNION ALL
			SELECT COALESCE(due_date, date), amount AS income, 0 AS expense FROM invoices
			WHERE  status='Completado' AND COALESCE(due_date, date) >= $1 AND COALESCE(due_date, date) <= $2
			UNION ALL
			SELECT COALESCE(due_date, date), 0 AS income, amount AS expense FROM purchases
			WHERE  status='Completado' AND COALESCE(due_date, date) >= $1 AND COALESCE(due_date, date) <= $2
		) sub
		GROUP  BY 1, 2
		ORDER  BY 1, 2`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.MonthlyTotal, 0)
	for rows.Next() {
		var mt domain.MonthlyTotal
		var month int
		if err := rows.Scan(&mt.Year, &month, &mt.Income, &mt.Expense); err != nil {
			return nil, err
		}
		mt.Month = time.Month(month)
		out = append(out, mt)
	}
	return out, rows.Err()
}

func (s *Store) GetDailyTotals(from, to time.Time) ([]domain.DailyTotal, error) {
	rows, err := s.pool.Query(bg(), `
		SELECT date::TEXT, COALESCE(SUM(income), 0), COALESCE(SUM(expense), 0)
		FROM (
			SELECT date, amount AS income, 0 AS expense FROM transactions
			WHERE  status='Completado' AND is_projection=false AND type='Ingreso'
			  AND  date >= $1 AND date <= $2
			UNION ALL
			SELECT date, 0 AS income, amount AS expense FROM transactions
			WHERE  status='Completado' AND is_projection=false AND type='Egreso'
			  AND  date >= $1 AND date <= $2
			UNION ALL
			SELECT COALESCE(due_date, date), amount AS income, 0 AS expense FROM invoices
			WHERE  status='Completado' AND COALESCE(due_date, date) >= $1 AND COALESCE(due_date, date) <= $2
			UNION ALL
			SELECT COALESCE(due_date, date), 0 AS income, amount AS expense FROM purchases
			WHERE  status='Completado' AND COALESCE(due_date, date) >= $1 AND COALESCE(due_date, date) <= $2
		) sub
		GROUP  BY date
		ORDER  BY date`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.DailyTotal, 0)
	for rows.Next() {
		var dt domain.DailyTotal
		if err := rows.Scan(&dt.Date, &dt.Ingresos, &dt.Egresos); err != nil {
			return nil, err
		}
		out = append(out, dt)
	}
	return out, rows.Err()
}

func (s *Store) GetPendingTransactions() ([]*domain.Transaction, error) {
	rows, err := s.pool.Query(bg(), `
		SELECT id, date::TEXT, description, category, type, amount, status,
		       COALESCE(reference,''), COALESCE(detail,''), source, COALESCE(external_id,''),
		       is_projection, created_at, updated_at
		FROM   transactions
		WHERE  status='Pendiente' AND is_projection=false
		ORDER  BY date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTransactions(rows)
}

func (s *Store) GetPendingProjections(horizon time.Time) ([]*domain.Transaction, error) {
	rows, err := s.pool.Query(bg(), `
		SELECT id, date::TEXT, description, category, type, amount, status,
		       COALESCE(reference,''), COALESCE(detail,''), source, COALESCE(external_id,''),
		       is_projection, created_at, updated_at
		FROM   transactions
		WHERE  is_projection=true AND status != 'Anulado' AND date <= $1
		ORDER  BY date ASC`, horizon)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTransactions(rows)
}

func (s *Store) GetCategoryTotals(from, to time.Time, txType domain.TransactionType) ([]domain.CategoryTotal, error) {
	var rows pgx.Rows
	var err error
	if txType == domain.TypeIngreso {
		rows, err = s.pool.Query(bg(), `
			SELECT category, COALESCE(SUM(amount), 0)
			FROM (
				SELECT category, amount FROM transactions
				WHERE  status='Completado' AND is_projection=false AND type='Ingreso'
				  AND  date >= $1 AND date <= $2
				UNION ALL
				SELECT category, amount FROM invoices
				WHERE  status='Completado' AND COALESCE(due_date, date) >= $1 AND COALESCE(due_date, date) <= $2
			) sub
			GROUP  BY category
			ORDER  BY 2 DESC`, from, to)
	} else {
		rows, err = s.pool.Query(bg(), `
			SELECT category, COALESCE(SUM(amount), 0)
			FROM (
				SELECT category, amount FROM transactions
				WHERE  status='Completado' AND is_projection=false AND type='Egreso'
				  AND  date >= $1 AND date <= $2
				UNION ALL
				SELECT category, amount FROM purchases
				WHERE  status='Completado' AND COALESCE(due_date, date) >= $1 AND COALESCE(due_date, date) <= $2
			) sub
			GROUP  BY category
			ORDER  BY 2 DESC`, from, to)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.CategoryTotal, 0)
	for rows.Next() {
		var ct domain.CategoryTotal
		if err := rows.Scan(&ct.Category, &ct.Amount); err != nil {
			return nil, err
		}
		out = append(out, ct)
	}
	return out, rows.Err()
}

func (s *Store) GetWeeklyTotals(year int, month time.Month) ([]domain.WeeklyComparison, error) {
	rows, err := s.pool.Query(bg(), `
		SELECT week, COALESCE(SUM(income), 0), COALESCE(SUM(expense), 0)
		FROM (
			SELECT (EXTRACT(DAY FROM date)::int - 1) / 7 + 1 AS week,
			       amount AS income, 0 AS expense
			FROM   transactions
			WHERE  status='Completado' AND is_projection=false AND type='Ingreso'
			  AND  EXTRACT(YEAR FROM date)::int = $1 AND EXTRACT(MONTH FROM date)::int = $2
			UNION ALL
			SELECT (EXTRACT(DAY FROM date)::int - 1) / 7 + 1 AS week,
			       0 AS income, amount AS expense
			FROM   transactions
			WHERE  status='Completado' AND is_projection=false AND type='Egreso'
			  AND  EXTRACT(YEAR FROM date)::int = $1 AND EXTRACT(MONTH FROM date)::int = $2
			UNION ALL
			SELECT (EXTRACT(DAY FROM COALESCE(due_date, date))::int - 1) / 7 + 1 AS week,
			       amount AS income, 0 AS expense
			FROM   invoices
			WHERE  status='Completado'
			  AND  EXTRACT(YEAR FROM COALESCE(due_date, date))::int = $1
			  AND  EXTRACT(MONTH FROM COALESCE(due_date, date))::int = $2
			UNION ALL
			SELECT (EXTRACT(DAY FROM COALESCE(due_date, date))::int - 1) / 7 + 1 AS week,
			       0 AS income, amount AS expense
			FROM   purchases
			WHERE  status='Completado'
			  AND  EXTRACT(YEAR FROM COALESCE(due_date, date))::int = $1
			  AND  EXTRACT(MONTH FROM COALESCE(due_date, date))::int = $2
		) sub
		GROUP  BY week
		ORDER  BY week`, year, int(month))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.WeeklyComparison, 0)
	for rows.Next() {
		var wc domain.WeeklyComparison
		if err := rows.Scan(&wc.Week, &wc.Ingresos, &wc.Egresos); err != nil {
			return nil, err
		}
		out = append(out, wc)
	}
	return out, rows.Err()
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
	if err := s.pool.QueryRow(bg(), `
		INSERT INTO users (id, name, email, role, password_hash, ms_oid, active)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),true)
		RETURNING created_at`,
		u.ID, u.Name, u.Email, string(u.Role), u.Password, u.MicrosoftOID,
	).Scan(&u.CreatedAt); err != nil {
		return err
	}
	_, err := s.pool.Exec(bg(), `
		INSERT INTO settings (user_id, key, value) VALUES
		  ($1, 'baseCurrency',     '"COP"'),
		  ($1, 'autoExchangeRate', 'true')
		ON CONFLICT (user_id, key) DO NOTHING`,
		u.ID)
	return err
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
	tag, err := s.pool.Exec(bg(), `
		UPDATE siigo_configs SET last_sync_at=$1, updated_at=now()
		WHERE  id = (SELECT id FROM siigo_configs ORDER BY created_at DESC LIMIT 1)`, t)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Config row was wiped (e.g. table clear) — insert a minimal placeholder so
		// future incremental syncs have a lastSync anchor.
		_, err = s.pool.Exec(bg(), `
			INSERT INTO siigo_configs (last_sync_at) VALUES ($1)
			ON CONFLICT DO NOTHING`, t)
	}
	return err
}

func (s *Store) GetEarliestSiigoDate() (string, error) {
	var d string
	err := s.pool.QueryRow(bg(), `
		SELECT TO_CHAR(MIN(min_date), 'YYYY-MM-DD') FROM (
			SELECT MIN(date) AS min_date FROM transactions WHERE source = 'Siigo'
			UNION ALL
			SELECT MIN(date) FROM invoices
			UNION ALL
			SELECT MIN(date) FROM purchases
		) sub`,
	).Scan(&d)
	if err != nil {
		return "", err
	}
	return d, nil
}

func (s *Store) GetOldestPendingOrPartialDate() (string, error) {
	var d string
	err := s.pool.QueryRow(bg(), `
		SELECT TO_CHAR(MIN(min_date), 'YYYY-MM-DD') FROM (
			SELECT MIN(date) AS min_date FROM invoices  WHERE status IN ('Pendiente', 'Parcial')
			UNION ALL
			SELECT MIN(date) FROM purchases WHERE status IN ('Pendiente', 'Parcial')
		) sub`,
	).Scan(&d)
	if err != nil {
		return "", err
	}
	return d, nil
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

// ── Invoices ──────────────────────────────────────────────────────────────────

const invoiceCols = `
	id, external_id, source, is_projection,
	COALESCE(reference,''), COALESCE(prefix,''), COALESCE(number,0), date::TEXT, COALESCE(due_date::TEXT,''),
	COALESCE(customer_identification,''), COALESCE(customer_name,''),
	amount, balance, status, category, COALESCE(detail,''),
	synced_at, created_at, updated_at`

func (s *Store) GetAllInvoices() ([]*domain.Invoice, error) {
	rows, err := s.pool.Query(bg(), `SELECT`+invoiceCols+` FROM invoices ORDER BY date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInvoices(rows)
}

func (s *Store) GetPendingInvoices() ([]*domain.Invoice, error) {
	rows, err := s.pool.Query(bg(), `SELECT`+invoiceCols+` FROM invoices WHERE status IN ('Pendiente','Parcial') ORDER BY date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInvoices(rows)
}

func (s *Store) GetInvoiceByID(id string) (*domain.Invoice, bool, error) {
	row := s.pool.QueryRow(bg(), `SELECT`+invoiceCols+` FROM invoices WHERE id=$1`, id)
	inv, err := scanInvoice(row)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	return inv, err == nil, err
}

func (s *Store) UpsertInvoice(inv *domain.Invoice) (bool, error) {
	newID := uuid.NewString()
	var prefix *string
	if inv.Prefix != "" {
		prefix = &inv.Prefix
	}
	var number *int
	if inv.Number != 0 {
		number = &inv.Number
	}
	var dueDate *string
	if inv.DueDate != "" {
		dueDate = &inv.DueDate
	}
	var actualID string
	var inserted bool
	ref := inv.Reference
	if ref == "" && inv.Prefix != "" && inv.Number != 0 {
		ref = inv.Prefix + "-" + fmt.Sprintf("%d", inv.Number)
	}
	err := s.pool.QueryRow(bg(), `
		INSERT INTO invoices
		  (id, external_id, source, is_projection, reference, prefix, number, date, due_date,
		   customer_identification, customer_name, amount, balance, status, category, detail, description, synced_at)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),$12,$13,$14,$15,$16,NULLIF($5,''),now())
		ON CONFLICT (external_id) DO UPDATE
		  SET reference=NULLIF($5,''), date=$8, due_date=$9,
		      customer_identification=NULLIF($10,''), customer_name=NULLIF($11,''),
		      amount=$12, balance=$13, status=$14, category=$15, detail=$16,
		      description=NULLIF($5,''),
		      synced_at=now(), updated_at=now()
		RETURNING id, (xmax = 0) AS inserted, created_at, updated_at, synced_at`,
		newID, inv.ExternalID, inv.Source, inv.IsProjection,
		ref, prefix, number, parseDate(inv.Date), dueDate,
		inv.CustomerIdentification, inv.CustomerName,
		inv.Total, inv.Balance, string(inv.Status), inv.Category, inv.Detail,
	).Scan(&actualID, &inserted, &inv.CreatedAt, &inv.UpdatedAt, &inv.SyncedAt)
	inv.ID = actualID
	return inserted, err
}

// ── Purchases ─────────────────────────────────────────────────────────────────

const purchaseCols = `
	id, external_id, source, is_projection,
	COALESCE(reference,''), COALESCE(prefix,''), COALESCE(number,0), date::TEXT, COALESCE(due_date::TEXT,''),
	COALESCE(provider_identification,''), COALESCE(provider_name,''),
	amount, balance, status, category, COALESCE(detail,''),
	synced_at, created_at, updated_at`

func (s *Store) GetAllPurchases() ([]*domain.Purchase, error) {
	rows, err := s.pool.Query(bg(), `SELECT`+purchaseCols+` FROM purchases ORDER BY date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPurchases(rows)
}

func (s *Store) GetPendingPurchases() ([]*domain.Purchase, error) {
	rows, err := s.pool.Query(bg(), `SELECT`+purchaseCols+` FROM purchases WHERE status IN ('Pendiente','Parcial') ORDER BY date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPurchases(rows)
}

func (s *Store) GetPurchaseByID(id string) (*domain.Purchase, bool, error) {
	row := s.pool.QueryRow(bg(), `SELECT`+purchaseCols+` FROM purchases WHERE id=$1`, id)
	pur, err := scanPurchase(row)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	return pur, err == nil, err
}

func (s *Store) UpsertPurchase(pur *domain.Purchase) (bool, error) {
	newID := uuid.NewString()
	var prefix *string
	if pur.Prefix != "" {
		prefix = &pur.Prefix
	}
	var number *int
	if pur.Number != 0 {
		number = &pur.Number
	}
	var dueDate *string
	if pur.DueDate != "" {
		dueDate = &pur.DueDate
	}
	var actualID string
	var inserted bool
	ref := pur.Reference
	if ref == "" && pur.Prefix != "" && pur.Number != 0 {
		ref = pur.Prefix + "-" + fmt.Sprintf("%d", pur.Number)
	}
	err := s.pool.QueryRow(bg(), `
		INSERT INTO purchases
		  (id, external_id, source, is_projection, reference, prefix, number, date, due_date,
		   provider_identification, provider_name, amount, balance, status, category, detail, description, synced_at)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),$12,$13,$14,$15,$16,NULLIF($5,''),now())
		ON CONFLICT (external_id) DO UPDATE
		  SET reference=NULLIF($5,''), date=$8, due_date=$9,
		      provider_identification=NULLIF($10,''), provider_name=NULLIF($11,''),
		      amount=$12, balance=$13, status=$14, category=$15, detail=$16,
		      description=NULLIF($5,''),
		      synced_at=now(), updated_at=now()
		RETURNING id, (xmax = 0) AS inserted, created_at, updated_at, synced_at`,
		newID, pur.ExternalID, pur.Source, pur.IsProjection,
		ref, prefix, number, parseDate(pur.Date), dueDate,
		pur.ProviderIdentification, pur.ProviderName,
		pur.Total, pur.Balance, string(pur.Status), pur.Category, pur.Detail,
	).Scan(&actualID, &inserted, &pur.CreatedAt, &pur.UpdatedAt, &pur.SyncedAt)
	pur.ID = actualID
	return inserted, err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanTransaction(row scanner) (*domain.Transaction, error) {
	var t domain.Transaction
	var txType, txStatus, txSource string
	err := row.Scan(&t.ID, &t.Date, &t.Description, &t.Category,
		&txType, &t.Amount, &txStatus, &t.Reference, &t.Detail,
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

func scanInvoice(row scanner) (*domain.Invoice, error) {
	var inv domain.Invoice
	var status string
	err := row.Scan(
		&inv.ID, &inv.ExternalID, &inv.Source, &inv.IsProjection,
		&inv.Reference, &inv.Prefix, &inv.Number, &inv.Date, &inv.DueDate,
		&inv.CustomerIdentification, &inv.CustomerName,
		&inv.Total, &inv.Balance, &status, &inv.Category, &inv.Detail,
		&inv.SyncedAt, &inv.CreatedAt, &inv.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	inv.Status = domain.TransactionStatus(status)
	return &inv, nil
}

func scanInvoices(rows pgx.Rows) ([]*domain.Invoice, error) {
	list := make([]*domain.Invoice, 0)
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, inv)
	}
	return list, rows.Err()
}

func scanPurchase(row scanner) (*domain.Purchase, error) {
	var pur domain.Purchase
	var status string
	err := row.Scan(
		&pur.ID, &pur.ExternalID, &pur.Source, &pur.IsProjection,
		&pur.Reference, &pur.Prefix, &pur.Number, &pur.Date, &pur.DueDate,
		&pur.ProviderIdentification, &pur.ProviderName,
		&pur.Total, &pur.Balance, &status, &pur.Category, &pur.Detail,
		&pur.SyncedAt, &pur.CreatedAt, &pur.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	pur.Status = domain.TransactionStatus(status)
	return &pur, nil
}

func scanPurchases(rows pgx.Rows) ([]*domain.Purchase, error) {
	list := make([]*domain.Purchase, 0)
	for rows.Next() {
		pur, err := scanPurchase(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, pur)
	}
	return list, rows.Err()
}

// ── Search ────────────────────────────────────────────────────────────────────

var prefixToTable = map[string]string{
	"FV": "invoices",
	"FC": "purchases",
	"RC": "transactions",
	"RP": "transactions",
}

func (s *Store) Search(reference string) ([]domain.SearchDocument, error) {
	upper := strings.ToUpper(strings.TrimSpace(reference))
	prefix := ""
	if len(upper) >= 2 {
		prefix = upper[:2]
	}

	table, ok := prefixToTable[prefix]
	if !ok {
		table = "transactions"
	}

	sql := fmt.Sprintf(
		`SELECT id, COALESCE(reference,''), date::TEXT, COALESCE(description,''), amount, status, category
		 FROM %s WHERE reference ILIKE $1 ORDER BY date DESC LIMIT 50`,
		table,
	)

	rows, err := s.pool.Query(bg(), sql, "%"+reference+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docType := prefix
	if !ok {
		docType = "TX"
	}

	out := make([]domain.SearchDocument, 0)
	for rows.Next() {
		var d domain.SearchDocument
		d.DocType = docType
		if err := rows.Scan(&d.ID, &d.Reference, &d.Date, &d.Description, &d.Amount, &d.Status, &d.Category); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
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
