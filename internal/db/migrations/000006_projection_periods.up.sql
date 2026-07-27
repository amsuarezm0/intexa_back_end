-- Custom projection horizons, managed by ADMINISTRADOR/GESTIÓN and shared by
-- all users. The built-in 30/60/90 day periods are not stored here; only the
-- extra ones a manager adds.
CREATE TABLE IF NOT EXISTS projection_periods (
	id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
	days       INTEGER     NOT NULL UNIQUE CHECK (days BETWEEN 1 AND 3650),
	label      TEXT        NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
