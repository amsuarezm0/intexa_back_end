-- ─────────────────────────────────────────────────────────────────────────────
-- Users & Auth
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS users (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT        NOT NULL,
    email         TEXT        UNIQUE NOT NULL,
    role          TEXT        NOT NULL DEFAULT 'CONSULTA'
                              CHECK (role IN ('ADMINISTRADOR', 'TESORERÍA', 'CONSULTA')),
    password_hash TEXT,
    ms_oid        TEXT        UNIQUE,
    ms_tenant     TEXT,
    active        BOOLEAN     NOT NULL DEFAULT true,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS allowed_domains (
    domain     TEXT        PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Siigo integration
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS siigo_configs (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_name        TEXT        NOT NULL,
    access_key_enc   TEXT        NOT NULL,
    partner_id       TEXT        NOT NULL DEFAULT '',
    token            TEXT,
    token_expires_at TIMESTAMPTZ,
    last_sync_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Financial data
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS categories (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        UNIQUE NOT NULL,
    type       TEXT        NOT NULL DEFAULT 'both'
                           CHECK (type IN ('income', 'expense', 'both')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS budget_lines (
    id         UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    category   TEXT          NOT NULL REFERENCES categories(name) ON UPDATE CASCADE,
    monthly    NUMERIC(18,2) NOT NULL,
    year       INTEGER       NOT NULL,
    month      INTEGER       NOT NULL CHECK (month BETWEEN 1 AND 12),
    created_at TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (category, year, month)
);

CREATE TABLE IF NOT EXISTS transactions (
    id            UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    date          DATE          NOT NULL,
    description   TEXT          NOT NULL,
    detail        TEXT          NOT NULL DEFAULT '',
    category      TEXT          NOT NULL,
    type          TEXT          NOT NULL CHECK (type IN ('Ingreso', 'Egreso')),
    amount        NUMERIC(18,2) NOT NULL,
    balance       NUMERIC(18,2) NOT NULL DEFAULT 0,
    status        TEXT          NOT NULL DEFAULT 'Pendiente'
                                CHECK (status IN ('Completado', 'Pendiente', 'Anulado', 'Parcial')),
    reference     TEXT,
    source        TEXT          NOT NULL DEFAULT 'Manual'
                                CHECK (source IN ('Siigo', 'Manual')),
    external_id   TEXT          UNIQUE,
    parent_id     UUID          REFERENCES transactions(id) ON DELETE CASCADE,
    is_projection BOOLEAN       NOT NULL DEFAULT false,
    created_by    UUID          REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS bank_balance (
    id         SERIAL      PRIMARY KEY,
    amount     NUMERIC(18,2) NOT NULL DEFAULT 0,
    updated_by TEXT          NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS settings (
    user_id    TEXT        NOT NULL DEFAULT '',
    key        TEXT        NOT NULL,
    value      JSONB       NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);

CREATE TABLE IF NOT EXISTS activity_logs (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        REFERENCES users(id) ON DELETE SET NULL,
    user_name  TEXT        NOT NULL,
    initial    TEXT        NOT NULL,
    action     TEXT        NOT NULL,
    module     TEXT        NOT NULL,
    color      TEXT,
    metadata   JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Indexes
-- ─────────────────────────────────────────────────────────────────────────────

CREATE INDEX IF NOT EXISTS idx_transactions_date        ON transactions(date);
CREATE INDEX IF NOT EXISTS idx_transactions_created_at  ON transactions(created_at);
CREATE INDEX IF NOT EXISTS idx_transactions_type        ON transactions(type);
CREATE INDEX IF NOT EXISTS idx_transactions_status      ON transactions(status);
CREATE INDEX IF NOT EXISTS idx_transactions_source      ON transactions(source);
CREATE INDEX IF NOT EXISTS idx_transactions_external_id ON transactions(external_id);
CREATE INDEX IF NOT EXISTS idx_transactions_category    ON transactions(category);
CREATE INDEX IF NOT EXISTS idx_activity_logs_created    ON activity_logs(created_at DESC);

-- ─────────────────────────────────────────────────────────────────────────────
-- Seed data
-- ─────────────────────────────────────────────────────────────────────────────

INSERT INTO categories (name, type) VALUES
    ('Operacional - Ventas',   'income'),
    ('Ingresos Editoriales',   'income'),
    ('Ingresos Directos',      'income'),
    ('Finanzas - Inversiones', 'income'),
    ('Gastos - Personal',      'expense'),
    ('Gastos - Tecnología',    'expense'),
    ('Gastos Operativos',      'expense'),
    ('Marketing',              'expense'),
    ('Infraestructura',        'expense')
ON CONFLICT (name) DO NOTHING;

