-- Installment schedule (from Siigo payments) for invoices and purchases.
ALTER TABLE invoices  ADD COLUMN IF NOT EXISTS installments JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS installments JSONB NOT NULL DEFAULT '[]'::jsonb;
