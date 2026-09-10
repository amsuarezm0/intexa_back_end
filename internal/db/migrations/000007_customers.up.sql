-- customers: terceros sincronizados desde Siigo (/v1/customers).
--
-- El endpoint devuelve clientes, proveedores y otros terceros; `type` los
-- distingue. La identificación NO es única — una empresa con varias sucursales
-- la repite una vez por sucursal — así que la identidad de un tercero es el par
-- (identification, branch_office), y sobre ese par se hace el upsert para que
-- una sincronización nunca duplique un cliente. `siigo_id` se guarda como
-- referencia (y se actualiza), pero no como clave: Siigo puede reemitirlo.
CREATE TABLE IF NOT EXISTS customers (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id      TEXT        NOT NULL,
    siigo_id         TEXT        NOT NULL DEFAULT '',
    type             TEXT        NOT NULL DEFAULT 'Cliente'
                                 CHECK (type IN ('Cliente', 'Proveedor', 'Otro')),
    person_type      TEXT        NOT NULL DEFAULT '',
    id_type          TEXT        NOT NULL DEFAULT '',
    identification   TEXT        NOT NULL,
    check_digit      TEXT        NOT NULL DEFAULT '',
    branch_office    INTEGER     NOT NULL DEFAULT 0,
    name             TEXT        NOT NULL DEFAULT '',
    commercial_name  TEXT        NOT NULL DEFAULT '',
    active           BOOLEAN     NOT NULL DEFAULT true,
    vat_responsible  BOOLEAN     NOT NULL DEFAULT false,
    address          TEXT        NOT NULL DEFAULT '',
    city             TEXT        NOT NULL DEFAULT '',
    state            TEXT        NOT NULL DEFAULT '',
    country          TEXT        NOT NULL DEFAULT '',
    postal_code      TEXT        NOT NULL DEFAULT '',
    email            TEXT        NOT NULL DEFAULT '',
    phone            TEXT        NOT NULL DEFAULT '',
    phones           JSONB       NOT NULL DEFAULT '[]'::jsonb,
    contacts         JSONB       NOT NULL DEFAULT '[]'::jsonb,
    -- Marcas de tiempo propias de Siigo: llegan sin zona horaria, se guardan
    -- como texto tal cual las entrega la API.
    siigo_created_at TEXT        NOT NULL DEFAULT '',
    siigo_updated_at TEXT        NOT NULL DEFAULT '',
    synced_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- La garantía de no duplicar: un tercero por identificación y sucursal.
CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_identity
    ON customers(identification, branch_office);

CREATE INDEX IF NOT EXISTS idx_customers_type     ON customers(type);
CREATE INDEX IF NOT EXISTS idx_customers_name     ON customers(lower(name));
CREATE INDEX IF NOT EXISTS idx_customers_siigo_id ON customers(siigo_id);
CREATE INDEX IF NOT EXISTS idx_customers_active   ON customers(active);
