DROP INDEX IF EXISTS idx_transactions_secondary_due;
DROP INDEX IF EXISTS idx_transactions_due_date;
DROP INDEX IF EXISTS idx_purchases_secondary_due;
DROP INDEX IF EXISTS idx_invoices_secondary_due;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS due_date,
    DROP COLUMN IF EXISTS secondary_due_date;
ALTER TABLE purchases DROP COLUMN IF EXISTS secondary_due_date;
ALTER TABLE invoices  DROP COLUMN IF EXISTS secondary_due_date;
