-- Align transactions with the new model: remove columns that belong to
-- invoices/purchases (balance, parent_id) and tighten the status check.
ALTER TABLE transactions DROP COLUMN IF EXISTS balance;
ALTER TABLE transactions DROP COLUMN IF EXISTS parent_id;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_status_check;
ALTER TABLE transactions ADD CONSTRAINT transactions_status_check
    CHECK (status IN ('Completado', 'Pendiente', 'Anulado'));
ALTER TABLE transactions ALTER COLUMN status SET DEFAULT 'Completado';

-- ─────────────────────────────────────────────────────────────────────────────
-- Invoices (FV — facturas de venta)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS invoices (
    id                      UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id             TEXT          UNIQUE NOT NULL,
    source                  TEXT          NOT NULL DEFAULT 'Siigo'
                                          CHECK (source IN ('Siigo')),
    is_projection           BOOLEAN       NOT NULL DEFAULT false,
    prefix                  TEXT,
    number                  INTEGER,
    date                    DATE          NOT NULL,
    due_date                DATE,
    customer_identification TEXT,
    customer_name           TEXT,
    total                   NUMERIC(18,2) NOT NULL,
    balance                 NUMERIC(18,2) NOT NULL DEFAULT 0,
    status                  TEXT          NOT NULL DEFAULT 'Pendiente'
                                          CHECK (status IN ('Completado', 'Pendiente', 'Anulado', 'Parcial')),
    category                TEXT          NOT NULL DEFAULT 'Operacional - Ventas',
    detail                  TEXT          NOT NULL DEFAULT '',
    synced_at               TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_at              TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Purchases (FC — facturas de compra)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS purchases (
    id                      UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id             TEXT          UNIQUE NOT NULL,
    source                  TEXT          NOT NULL DEFAULT 'Siigo'
                                          CHECK (source IN ('Siigo')),
    is_projection           BOOLEAN       NOT NULL DEFAULT false,
    prefix                  TEXT,
    number                  INTEGER,
    date                    DATE          NOT NULL,
    due_date                DATE,
    provider_identification TEXT,
    provider_name           TEXT,
    total                   NUMERIC(18,2) NOT NULL,
    balance                 NUMERIC(18,2) NOT NULL DEFAULT 0,
    status                  TEXT          NOT NULL DEFAULT 'Pendiente'
                                          CHECK (status IN ('Completado', 'Pendiente', 'Anulado', 'Parcial')),
    category                TEXT          NOT NULL DEFAULT 'Gastos Operativos',
    detail                  TEXT          NOT NULL DEFAULT '',
    synced_at               TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_at              TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Indexes
-- ─────────────────────────────────────────────────────────────────────────────

CREATE INDEX IF NOT EXISTS idx_invoices_date    ON invoices(date);
CREATE INDEX IF NOT EXISTS idx_invoices_status  ON invoices(status);
CREATE INDEX IF NOT EXISTS idx_invoices_customer ON invoices(customer_identification);

CREATE INDEX IF NOT EXISTS idx_purchases_date     ON purchases(date);
CREATE INDEX IF NOT EXISTS idx_purchases_status   ON purchases(status);
CREATE INDEX IF NOT EXISTS idx_purchases_provider ON purchases(provider_identification);
