-- Fecha de pago acordada (secondary_due_date).
--
-- Cuándo se va a pagar realmente, cuando difiere de lo que dice el documento.
-- Es el único campo editable a mano en un documento traído de Siigo: el resto
-- sigue siendo de solo lectura y lo reescribe cada sincronización.
--
-- NUNCA se sobreescribe desde Siigo. Los upserts de invoices/purchases listan
-- sus columnas una por una en el DO UPDATE SET, así que basta con no incluir
-- esta; hay una prueba que lo fija, porque agregarla ahí borraría en silencio
-- todas las fechas acordadas en la siguiente sincronización.
ALTER TABLE invoices  ADD COLUMN IF NOT EXISTS secondary_due_date DATE;
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS secondary_due_date DATE;

-- Los movimientos manuales no tenían vencimiento: se usaba `date` como si lo
-- fuera. Ahora llevan uno propio (opcional) y, aparte, la fecha acordada.
ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS due_date           DATE,
    ADD COLUMN IF NOT EXISTS secondary_due_date DATE;

CREATE INDEX IF NOT EXISTS idx_invoices_secondary_due     ON invoices(secondary_due_date);
CREATE INDEX IF NOT EXISTS idx_purchases_secondary_due    ON purchases(secondary_due_date);
CREATE INDEX IF NOT EXISTS idx_transactions_due_date      ON transactions(due_date);
CREATE INDEX IF NOT EXISTS idx_transactions_secondary_due ON transactions(secondary_due_date);
