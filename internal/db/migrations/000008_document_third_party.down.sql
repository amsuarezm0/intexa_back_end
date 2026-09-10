DROP INDEX IF EXISTS idx_transactions_counterparty;
DROP INDEX IF EXISTS idx_purchases_provider_branch;
DROP INDEX IF EXISTS idx_invoices_customer_branch;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS counterparty_identification,
    DROP COLUMN IF EXISTS counterparty_branch_office,
    DROP COLUMN IF EXISTS counterparty_siigo_id;

ALTER TABLE purchases
    DROP COLUMN IF EXISTS provider_branch_office,
    DROP COLUMN IF EXISTS provider_siigo_id;

ALTER TABLE invoices
    DROP COLUMN IF EXISTS customer_branch_office,
    DROP COLUMN IF EXISTS customer_siigo_id;
