-- Per-bank balance breakdown. `amount` remains the total (sum of accounts).
ALTER TABLE bank_balance ADD COLUMN IF NOT EXISTS accounts JSONB NOT NULL DEFAULT '[]'::jsonb;
