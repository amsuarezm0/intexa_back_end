DROP TABLE IF EXISTS purchases;
DROP TABLE IF EXISTS invoices;

ALTER TABLE transactions ADD COLUMN IF NOT EXISTS balance   NUMERIC(18,2) NOT NULL DEFAULT 0;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS parent_id UUID REFERENCES transactions(id) ON DELETE CASCADE;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_status_check;
ALTER TABLE transactions ADD CONSTRAINT transactions_status_check
    CHECK (status IN ('Completado', 'Pendiente', 'Anulado', 'Parcial'));
ALTER TABLE transactions ALTER COLUMN status SET DEFAULT 'Pendiente';
