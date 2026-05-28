-- ─────────────────────────────────────────────────────────────────────────────
-- fn_monthly_cashflow
-- Returns ingresos, egresos and net saldo for each of the last p_months months.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION fn_monthly_cashflow(p_months INT DEFAULT 7)
RETURNS TABLE(
    month_label TEXT,
    yr          INT,
    mo          INT,
    ingresos    NUMERIC,
    egresos     NUMERIC,
    saldo       NUMERIC
)
LANGUAGE sql STABLE AS $$
    SELECT
        TO_CHAR(m, 'Mon')                                              AS month_label,
        EXTRACT(YEAR  FROM m)::INT                                     AS yr,
        EXTRACT(MONTH FROM m)::INT                                     AS mo,
        COALESCE(SUM(CASE WHEN t.type = 'Ingreso' THEN t.amount END), 0) AS ingresos,
        COALESCE(SUM(CASE WHEN t.type = 'Egreso'  THEN t.amount END), 0) AS egresos,
        COALESCE(SUM(CASE WHEN t.type = 'Ingreso' THEN t.amount
                          WHEN t.type = 'Egreso'  THEN -t.amount END), 0) AS saldo
    FROM generate_series(
        date_trunc('month', now()) - ((p_months - 1) * INTERVAL '1 month'),
        date_trunc('month', now()),
        INTERVAL '1 month'
    ) AS m
    LEFT JOIN transactions t
           ON date_trunc('month', t.created_at) = m
          AND t.is_projection = false
          AND t.status <> 'Cancelado'
    GROUP BY m
    ORDER BY m;
$$;

-- ─────────────────────────────────────────────────────────────────────────────
-- fn_dashboard_stats
-- Aggregated stats for a specific year/month (defaults to current month).
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION fn_dashboard_stats(
    p_year  INT DEFAULT NULL,
    p_month INT DEFAULT NULL
)
RETURNS JSON
LANGUAGE plpgsql STABLE AS $$
DECLARE
    v_year  INT := COALESCE(p_year,  EXTRACT(YEAR  FROM now())::INT);
    v_month INT := COALESCE(p_month, EXTRACT(MONTH FROM now())::INT);
    v_prev_date DATE := (make_date(v_year, v_month, 1) - INTERVAL '1 month')::DATE;

    v_balance       NUMERIC;
    v_month_income  NUMERIC;
    v_month_expense NUMERIC;
    v_prev_income   NUMERIC;
    v_prev_expense  NUMERIC;
BEGIN
    SELECT COALESCE(SUM(CASE WHEN type = 'Ingreso' THEN amount ELSE -amount END), 0)
    INTO   v_balance
    FROM   transactions
    WHERE  status = 'Completado' AND is_projection = false;

    SELECT COALESCE(SUM(CASE WHEN type = 'Ingreso' THEN amount ELSE 0 END), 0),
           COALESCE(SUM(CASE WHEN type = 'Egreso'  THEN amount ELSE 0 END), 0)
    INTO   v_month_income, v_month_expense
    FROM   transactions
    WHERE  EXTRACT(YEAR  FROM created_at) = v_year
      AND  EXTRACT(MONTH FROM created_at) = v_month
      AND  is_projection = false;

    SELECT COALESCE(SUM(CASE WHEN type = 'Ingreso' THEN amount ELSE 0 END), 0),
           COALESCE(SUM(CASE WHEN type = 'Egreso'  THEN amount ELSE 0 END), 0)
    INTO   v_prev_income, v_prev_expense
    FROM   transactions
    WHERE  EXTRACT(YEAR  FROM created_at) = EXTRACT(YEAR  FROM v_prev_date)
      AND  EXTRACT(MONTH FROM created_at) = EXTRACT(MONTH FROM v_prev_date)
      AND  is_projection = false;

    RETURN json_build_object(
        'balance',       v_balance,
        'monthIncome',   v_month_income,
        'monthExpense',  v_month_expense,
        'prevIncome',    v_prev_income,
        'prevExpense',   v_prev_expense,
        'netFlow',       v_month_income - v_month_expense
    );
END;
$$;

-- ─────────────────────────────────────────────────────────────────────────────
-- fn_expense_breakdown
-- Egresos by category for the last p_months months, as amount + percentage.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION fn_expense_breakdown(p_months INT DEFAULT 3)
RETURNS TABLE(
    category   TEXT,
    total      NUMERIC,
    percentage NUMERIC
)
LANGUAGE sql STABLE AS $$
    WITH totals AS (
        SELECT
            t.category,
            SUM(t.amount) AS cat_total
        FROM transactions t
        WHERE t.type = 'Egreso'
          AND t.is_projection = false
          AND t.status <> 'Cancelado'
          AND t.created_at >= date_trunc('month', now())
                            - ((p_months - 1) * INTERVAL '1 month')
        GROUP BY t.category
    ),
    grand AS (SELECT NULLIF(SUM(cat_total), 0) AS grand_total FROM totals)
    SELECT
        tot.category,
        tot.cat_total,
        ROUND(tot.cat_total / g.grand_total * 100, 1)
    FROM totals tot, grand g
    ORDER BY tot.cat_total DESC;
$$;

-- ─────────────────────────────────────────────────────────────────────────────
-- fn_budget_deviation
-- Actual vs budget for every category in a given month.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION fn_budget_deviation(
    p_year  INT DEFAULT NULL,
    p_month INT DEFAULT NULL
)
RETURNS TABLE(
    category    TEXT,
    budget      NUMERIC,
    actual      NUMERIC,
    deviation   NUMERIC,
    is_positive BOOLEAN
)
LANGUAGE sql STABLE AS $$
    WITH params AS (
        SELECT
            COALESCE(p_year,  EXTRACT(YEAR  FROM now())::INT) AS yr,
            COALESCE(p_month, EXTRACT(MONTH FROM now())::INT) AS mo
    ),
    actuals AS (
        SELECT t.category, SUM(t.amount) AS actual_amount
        FROM   transactions t, params p
        WHERE  EXTRACT(YEAR  FROM t.created_at) = p.yr
          AND  EXTRACT(MONTH FROM t.created_at) = p.mo
          AND  t.is_projection = false
          AND  t.status <> 'Cancelado'
        GROUP BY t.category
    )
    SELECT
        b.category,
        b.monthly                           AS budget,
        COALESCE(a.actual_amount, 0)        AS actual,
        b.monthly - COALESCE(a.actual_amount, 0) AS deviation,
        (b.monthly - COALESCE(a.actual_amount, 0)) >= 0 AS is_positive
    FROM budget_lines b
    CROSS JOIN params p
    LEFT JOIN actuals a ON a.category = b.category
    WHERE b.year = p.yr AND b.month = p.mo
    ORDER BY actual DESC NULLS LAST;
$$;

-- ─────────────────────────────────────────────────────────────────────────────
-- fn_balance_projection
-- Running balance sampled day-by-day for the next p_days days, starting from
-- the current completed-transaction balance plus pending/projection flows.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION fn_balance_projection(p_days INT DEFAULT 30)
RETURNS TABLE(
    day_offset INT,
    day_date   DATE,
    balance    NUMERIC,
    deficit    NUMERIC
)
LANGUAGE plpgsql STABLE AS $$
DECLARE
    v_base NUMERIC;
BEGIN
    SELECT COALESCE(SUM(CASE WHEN type = 'Ingreso' THEN amount ELSE -amount END), 0)
    INTO   v_base
    FROM   transactions
    WHERE  status = 'Completado' AND is_projection = false;

    RETURN QUERY
    WITH daily_flows AS (
        SELECT
            GREATEST((t.created_at::DATE - CURRENT_DATE), 0) AS day_off,
            SUM(CASE WHEN t.type = 'Ingreso' THEN t.amount ELSE -t.amount END) AS net
        FROM transactions t
        WHERE (t.status = 'Pendiente' OR t.is_projection = true)
          AND t.status <> 'Cancelado'
          AND t.created_at::DATE BETWEEN CURRENT_DATE
                                     AND CURRENT_DATE + p_days
        GROUP BY day_off
    ),
    series AS (
        SELECT generate_series(0, p_days) AS d
    )
    SELECT
        s.d::INT,
        (CURRENT_DATE + s.d)::DATE,
        GREATEST(v_base + COALESCE(SUM(f.net) OVER (ORDER BY s.d
                                                    ROWS BETWEEN UNBOUNDED PRECEDING
                                                             AND CURRENT ROW), 0), 0),
        GREATEST(-(v_base + COALESCE(SUM(f.net) OVER (ORDER BY s.d
                                                      ROWS BETWEEN UNBOUNDED PRECEDING
                                                               AND CURRENT ROW), 0)), 0)
    FROM series s
    LEFT JOIN daily_flows f ON f.day_off = s.d
    ORDER BY s.d;
END;
$$;

-- ─────────────────────────────────────────────────────────────────────────────
-- fn_pending_alerts
-- Overdue pending transactions (>= p_min_days old), largest first.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION fn_pending_alerts(
    p_min_days INT DEFAULT 2,
    p_limit    INT DEFAULT 5
)
RETURNS TABLE(
    id          UUID,
    alert_type  TEXT,
    title       TEXT,
    description TEXT,
    amount      NUMERIC,
    due_date    TEXT,
    tx_type     TEXT
)
LANGUAGE sql STABLE AS $$
    SELECT
        t.id,
        CASE WHEN now() - t.created_at > INTERVAL '10 days' THEN 'danger'
             ELSE 'warning'
        END,
        t.description,
        t.category || ' · Pendiente hace '
            || EXTRACT(DAY FROM now() - t.created_at)::INT || ' días',
        t.amount,
        t.date::TEXT,
        t.type
    FROM transactions t
    WHERE t.status = 'Pendiente'
      AND t.is_projection = false
      AND now() - t.created_at >= (p_min_days * INTERVAL '1 day')
    ORDER BY t.amount DESC
    LIMIT p_limit;
$$;

-- ─────────────────────────────────────────────────────────────────────────────
-- upsert_siigo_invoice
-- Insert or update a Siigo sales invoice and mirror it into transactions.
-- The external_id guard prevents duplicate transaction rows across syncs.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE PROCEDURE upsert_siigo_invoice(
    p_siigo_id   INTEGER,
    p_doc_id     INTEGER,
    p_prefix     TEXT,
    p_number     INTEGER,
    p_date       DATE,
    p_due_date   DATE,
    p_cust_id    TEXT,
    p_cust_name  TEXT,
    p_seller_id  INTEGER,
    p_total      NUMERIC,
    p_balance    NUMERIC,
    p_obs        TEXT,
    p_stamp      TEXT,
    p_raw        JSONB
)
LANGUAGE plpgsql AS $$
DECLARE
    v_ext_id TEXT := 'siigo-inv-' || p_siigo_id;
    v_status TEXT := CASE WHEN p_balance = 0 THEN 'Completado' ELSE 'Pendiente' END;
    v_desc   TEXT := 'Factura ' || COALESCE(p_prefix,'') || '-' || p_number
                  || CASE WHEN p_cust_name IS NOT NULL THEN ' — ' || p_cust_name ELSE '' END;
BEGIN
    -- Upsert raw Siigo invoice
    INSERT INTO siigo_invoices (
        siigo_id, document_id, prefix, number, date, due_date,
        customer_identification, customer_name, seller_id,
        total, balance, observations, stamp_uuid, raw, synced_at
    ) VALUES (
        p_siigo_id, p_doc_id, p_prefix, p_number, p_date, p_due_date,
        p_cust_id, p_cust_name, p_seller_id,
        p_total, p_balance, p_obs, p_stamp, p_raw, now()
    )
    ON CONFLICT (siigo_id) DO UPDATE SET
        balance      = EXCLUDED.balance,
        observations = EXCLUDED.observations,
        stamp_uuid   = EXCLUDED.stamp_uuid,
        raw          = EXCLUDED.raw,
        synced_at    = now(),
        updated_at   = now();

    -- Insert transaction only if not already present
    INSERT INTO transactions (date, description, category, type, amount, status, reference, source, external_id)
    SELECT p_date, v_desc, 'Operacional - Ventas', 'Ingreso', p_total, v_status,
           COALESCE(p_prefix,'') || '-' || p_number, 'Siigo', v_ext_id
    WHERE NOT EXISTS (SELECT 1 FROM transactions WHERE external_id = v_ext_id);

    -- Keep status in sync on subsequent syncs
    UPDATE transactions
    SET    status = v_status, updated_at = now()
    WHERE  external_id = v_ext_id;
END;
$$;

-- ─────────────────────────────────────────────────────────────────────────────
-- upsert_siigo_purchase
-- Insert or update a Siigo purchase and mirror it into transactions.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE PROCEDURE upsert_siigo_purchase(
    p_siigo_id    INTEGER,
    p_doc_id      INTEGER,
    p_prefix      TEXT,
    p_number      INTEGER,
    p_date        DATE,
    p_due_date    DATE,
    p_prov_id     TEXT,
    p_prov_name   TEXT,
    p_total       NUMERIC,
    p_balance     NUMERIC,
    p_obs         TEXT,
    p_raw         JSONB
)
LANGUAGE plpgsql AS $$
DECLARE
    v_ext_id TEXT := 'siigo-pur-' || p_siigo_id;
    v_status TEXT := CASE WHEN p_balance = 0 THEN 'Completado' ELSE 'Pendiente' END;
    v_desc   TEXT := 'Factura Proveedor ' || COALESCE(p_prefix,'') || '-' || p_number
                  || CASE WHEN p_prov_name IS NOT NULL THEN ' — ' || p_prov_name ELSE '' END;
BEGIN
    INSERT INTO siigo_purchases (
        siigo_id, document_id, prefix, number, date, due_date,
        provider_identification, provider_name,
        total, balance, observations, raw, synced_at
    ) VALUES (
        p_siigo_id, p_doc_id, p_prefix, p_number, p_date, p_due_date,
        p_prov_id, p_prov_name,
        p_total, p_balance, p_obs, p_raw, now()
    )
    ON CONFLICT (siigo_id) DO UPDATE SET
        balance    = EXCLUDED.balance,
        raw        = EXCLUDED.raw,
        synced_at  = now(),
        updated_at = now();

    INSERT INTO transactions (date, description, category, type, amount, status, reference, source, external_id)
    SELECT p_date, v_desc, 'Gastos Operativos', 'Egreso', p_total, v_status,
           COALESCE(p_prefix,'') || '-' || p_number, 'Siigo', v_ext_id
    WHERE NOT EXISTS (SELECT 1 FROM transactions WHERE external_id = v_ext_id);

    UPDATE transactions
    SET    status = v_status, updated_at = now()
    WHERE  external_id = v_ext_id;
END;
$$;
