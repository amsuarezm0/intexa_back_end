-- ─────────────────────────────────────────────────────────────────────────────
-- Users & Auth
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS users (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT        NOT NULL,
    email         TEXT        UNIQUE NOT NULL,
    role          TEXT        NOT NULL DEFAULT 'CONSULTA'
                              CHECK (role IN ('ADMINISTRADOR', 'TESORERÍA', 'CONSULTA')),
    password_hash TEXT,                           -- NULL for Microsoft-only accounts
    ms_oid        TEXT        UNIQUE,             -- Microsoft Object ID (sub claim)
    ms_tenant     TEXT,
    active        BOOLEAN     NOT NULL DEFAULT true,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- CSRF tokens for Microsoft OAuth flow (one-time, short-lived)
CREATE TABLE IF NOT EXISTS oauth_states (
    state      TEXT        PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Email-domain allowlist for Microsoft login
-- Insert e.g. ('intexa.com') to allow all @intexa.com Microsoft accounts
CREATE TABLE IF NOT EXISTS allowed_domains (
    domain     TEXT        PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Siigo integration
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS siigo_configs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_name       TEXT        NOT NULL,
    access_key_enc  TEXT        NOT NULL,   -- AES-256-GCM encrypted
    partner_id      TEXT        NOT NULL DEFAULT '',
    token           TEXT,
    token_expires_at TIMESTAMPTZ,
    last_sync_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS siigo_customers (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    identification   TEXT        UNIQUE NOT NULL,
    id_type          TEXT,
    person_type      TEXT,
    name             TEXT        NOT NULL,
    commercial_name  TEXT,
    email            TEXT,
    phone            TEXT,
    active           BOOLEAN     NOT NULL DEFAULT true,
    raw              JSONB,
    synced_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS siigo_products (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    code                TEXT        UNIQUE NOT NULL,
    name                TEXT        NOT NULL,
    account_group       INTEGER,
    type                TEXT,
    tax_classification  TEXT,
    active              BOOLEAN     NOT NULL DEFAULT true,
    price               NUMERIC(18,2),
    currency            TEXT        NOT NULL DEFAULT 'COP',
    raw                 JSONB,
    synced_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS siigo_invoices (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    siigo_id                INTEGER     UNIQUE NOT NULL,
    document_id             INTEGER,
    prefix                  TEXT,
    number                  INTEGER,
    date                    DATE        NOT NULL,
    due_date                DATE,
    customer_identification TEXT,
    customer_name           TEXT,
    seller_id               INTEGER,
    total                   NUMERIC(18,2) NOT NULL,
    balance                 NUMERIC(18,2) NOT NULL DEFAULT 0,
    observations            TEXT,
    stamp_uuid              TEXT,
    raw                     JSONB,
    synced_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS siigo_invoice_items (
    id           UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id   UUID          NOT NULL REFERENCES siigo_invoices(id) ON DELETE CASCADE,
    product_code TEXT,
    description  TEXT,
    quantity     NUMERIC(12,4),
    unit_price   NUMERIC(18,2),
    discount     NUMERIC(6,2)  NOT NULL DEFAULT 0,
    total        NUMERIC(18,2),
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS siigo_purchases (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    siigo_id                INTEGER     UNIQUE NOT NULL,
    document_id             INTEGER,
    prefix                  TEXT,
    number                  INTEGER,
    date                    DATE        NOT NULL,
    due_date                DATE,
    provider_identification TEXT,
    provider_name           TEXT,
    total                   NUMERIC(18,2) NOT NULL,
    balance                 NUMERIC(18,2) NOT NULL DEFAULT 0,
    observations            TEXT,
    raw                     JSONB,
    synced_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS siigo_purchase_items (
    id            UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    purchase_id   UUID          NOT NULL REFERENCES siigo_purchases(id) ON DELETE CASCADE,
    product_code  TEXT,
    description   TEXT,
    quantity      NUMERIC(12,4),
    unit_price    NUMERIC(18,2),
    discount      NUMERIC(6,2)  NOT NULL DEFAULT 0,
    total         NUMERIC(18,2),
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now()
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
    category      TEXT          NOT NULL,
    type          TEXT          NOT NULL CHECK (type IN ('Ingreso', 'Egreso')),
    amount        NUMERIC(18,2) NOT NULL,
    status        TEXT          NOT NULL DEFAULT 'Pendiente'
                                CHECK (status IN ('Completado', 'Pendiente', 'Cancelado')),
    reference     TEXT,
    source        TEXT          NOT NULL DEFAULT 'Manual'
                                CHECK (source IN ('Siigo', 'Manual')),
    external_id   TEXT          UNIQUE,   -- siigo-inv-{id} / siigo-pur-{id}
    is_projection BOOLEAN       NOT NULL DEFAULT false,
    created_by    UUID          REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now()
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

CREATE TABLE IF NOT EXISTS settings (
    key        TEXT        PRIMARY KEY,
    value      JSONB       NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
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

CREATE INDEX IF NOT EXISTS idx_siigo_invoices_date      ON siigo_invoices(date);
CREATE INDEX IF NOT EXISTS idx_siigo_purchases_date     ON siigo_purchases(date);
CREATE INDEX IF NOT EXISTS idx_activity_logs_created    ON activity_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_oauth_states_expires     ON oauth_states(expires_at);

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

INSERT INTO settings (key, value) VALUES
    ('baseCurrency',     '"COP"'),
    ('autoExchangeRate', 'true')
ON CONFLICT (key) DO NOTHING;
