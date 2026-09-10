-- Referencia al tercero en todos los documentos.
--
-- Cada endpoint de Siigo entrega el mismo bloque escueto para su contraparte
-- ({id, identification, branch_office}) y nunca el nombre. Guardamos esa llave
-- en cada documento; el nombre se resuelve contra la tabla customers al leer,
-- de modo que nunca queda una copia desactualizada.
--
-- La identidad de un tercero es (identification, branch_office): un mismo NIT
-- se repite una vez por sucursal. Se guarda además el UUID de Siigo como
-- referencia exacta, aunque no se use como llave porque Siigo puede reemitirlo.

ALTER TABLE invoices
    ADD COLUMN IF NOT EXISTS customer_branch_office INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS customer_siigo_id      TEXT    NOT NULL DEFAULT '';

ALTER TABLE purchases
    ADD COLUMN IF NOT EXISTS provider_branch_office INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS provider_siigo_id      TEXT    NOT NULL DEFAULT '';

-- Los RC y RP viven en transactions y hasta ahora no guardaban contraparte
-- alguna, pese a que Siigo la entrega. "counterparty" y no "customer" porque
-- la misma columna sirve para el cliente de un RC y el proveedor de un RP.
ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS counterparty_identification TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS counterparty_branch_office  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS counterparty_siigo_id       TEXT    NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_invoices_customer_branch
    ON invoices(customer_identification, customer_branch_office);
CREATE INDEX IF NOT EXISTS idx_purchases_provider_branch
    ON purchases(provider_identification, provider_branch_office);
CREATE INDEX IF NOT EXISTS idx_transactions_counterparty
    ON transactions(counterparty_identification, counterparty_branch_office);
