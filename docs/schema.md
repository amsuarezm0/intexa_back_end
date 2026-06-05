# Schema & Data Flow

## Modelo de datos

```mermaid
erDiagram
    users {
        UUID id PK
        TEXT name
        TEXT email
        TEXT role
        TEXT password_hash
        TEXT ms_oid
        BOOLEAN active
        TIMESTAMPTZ created_at
    }

    transactions {
        UUID id PK
        DATE date
        TEXT description
        TEXT category
        TEXT type
        NUMERIC amount
        TEXT status
        TEXT source
        BOOLEAN is_projection
        TEXT external_id
        TEXT reference
        TEXT detail
        UUID created_by FK
        TIMESTAMPTZ created_at
        TIMESTAMPTZ updated_at
    }

    invoices {
        UUID id PK
        TEXT external_id
        TEXT source
        BOOLEAN is_projection
        TEXT prefix
        INTEGER number
        DATE date
        DATE due_date
        TEXT customer_identification
        TEXT customer_name
        NUMERIC total
        NUMERIC balance
        TEXT status
        TEXT category
        TEXT detail
        TIMESTAMPTZ synced_at
        TIMESTAMPTZ created_at
        TIMESTAMPTZ updated_at
    }

    purchases {
        UUID id PK
        TEXT external_id
        TEXT source
        BOOLEAN is_projection
        TEXT prefix
        INTEGER number
        DATE date
        DATE due_date
        TEXT provider_identification
        TEXT provider_name
        NUMERIC total
        NUMERIC balance
        TEXT status
        TEXT category
        TEXT detail
        TIMESTAMPTZ synced_at
        TIMESTAMPTZ created_at
        TIMESTAMPTZ updated_at
    }

    users ||--o{ transactions : "created_by"
```

---

## Flujo de datos

```mermaid
flowchart TD
    SIIGO[(Siigo API)]

    SIIGO -->|GET /v1/invoices| SYNC_FV[Sync FV]
    SIIGO -->|GET /v1/purchases| SYNC_FC[Sync FC]
    SIIGO -->|GET /v1/vouchers| SYNC_RC[Sync RC]
    SIIGO -->|GET /v1/payment-receipts| SYNC_RP[Sync RP]

    SYNC_FV -->|upsert por external_id| INV[(invoices)]
    SYNC_FC -->|upsert por external_id| PUR[(purchases)]
    SYNC_RC -->|upsert por external_id| TRX[(transactions\nsource=Siigo\nis_projection=false)]
    SYNC_RP -->|upsert por external_id| TRX

    USER([Usuario]) -->|ingreso / egreso real| MANUAL_REAL[Manual real]
    USER -->|proyección hipotética| MANUAL_PROJ[Manual proyectado]

    MANUAL_REAL -->|source=Manual\nis_projection=false| TRX
    MANUAL_PROJ -->|source=Manual\nis_projection=true| TRX
```

---

## Cálculo de saldos

```mermaid
flowchart LR
    TRX[(transactions)]
    INV[(invoices)]
    PUR[(purchases)]

    TRX -->|RC − RP + Manual real| REAL[Saldo real de caja]

    INV -->|SUM balance\nPendiente + Parcial| PROJ_IN[Ingresos proyectados]
    PUR -->|SUM balance\nPendiente + Parcial| PROJ_OUT[Egresos proyectados]
    TRX -->|SUM Manual\nis_projection=true| PROJ_MAN[Proyecciones manuales]

    REAL --> PROJECTED[Saldo proyectado]
    PROJ_IN --> PROJECTED
    PROJ_OUT --> PROJECTED
    PROJ_MAN --> PROJECTED
```

---

## Fuentes y roles

| Fuente | Tabla | `source` | `is_projection` | Rol |
|---|---|---|---|---|
| Siigo `/v1/invoices` | `invoices` | `Siigo` | `false` | Cartera por cobrar |
| Siigo `/v1/purchases` | `purchases` | `Siigo` | `false` | Cuentas por pagar |
| Siigo `/v1/vouchers` | `transactions` | `Siigo` | `false` | Cobro real de cliente |
| Siigo `/v1/payment-receipts` | `transactions` | `Siigo` | `false` | Pago real a proveedor |
| Usuario manual | `transactions` | `Manual` | `false` | Movimiento real sin factura |
| Usuario manual | `transactions` | `Manual` | `true` | Proyección hipotética |

---

## Estados por tabla

**`invoices` y `purchases`** — reflejan el estado de pago en Siigo. `source = 'Siigo'` bloquea edición y eliminación por el usuario. `is_projection` es siempre `false` — son obligaciones reales, no hipótesis:

| `status` | `balance` | Significado |
|---|---|---|
| `Pendiente` | `= total` | Sin pagos recibidos |
| `Parcial` | `> 0 y < total` | Pagos parciales recibidos |
| `Completado` | `= 0` | Totalmente pagado |
| `Anulado` | — | Documento anulado en Siigo |

**`transactions`** — estado del movimiento de caja. `source = 'Siigo'` bloquea edición y eliminación por el usuario. RC y RP se distinguen por `type` (Ingreso / Egreso). `is_projection` es siempre `false` para registros de Siigo — son movimientos reales de caja:

| `source` | `type` | `is_projection` | `status` | Significado |
|---|---|---|---|---|
| `Siigo` | `Ingreso` | `false` | `Completado` | Cobro real recibido (RC) |
| `Siigo` | `Egreso` | `false` | `Completado` | Pago real ejecutado (RP) |
| `Manual` | `Ingreso` / `Egreso` | `false` | `Completado` | Movimiento real ingresado por usuario |
| `Manual` | `Ingreso` / `Egreso` | `true` | `Pendiente` | Proyección hipotética futura |
| `Siigo` / `Manual` | — | — | `Anulado` | Cancelado |
