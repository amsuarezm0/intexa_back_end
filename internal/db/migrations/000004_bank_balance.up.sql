CREATE TABLE IF NOT EXISTS bank_balance (
    id         SERIAL PRIMARY KEY,
    amount     NUMERIC(18,2) NOT NULL DEFAULT 0,
    updated_by TEXT          NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ   NOT NULL DEFAULT now()
);

INSERT INTO bank_balance (amount, updated_by) VALUES (0, 'Sistema')
    ON CONFLICT DO NOTHING;
