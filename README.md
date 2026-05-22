# Arca API

REST API for treasury management built with Go. Tracks transactions, generates cash flow reports, integrates with Siigo accounting software, and provides financial dashboards.

## Tech Stack

- **Go** with [Chi](https://github.com/go-chi/chi) router
- **PostgreSQL** via `pgx/v5` (falls back to in-memory store if `DATABASE_URL` is unset)
- **JWT** authentication + Microsoft OAuth 2.0
- **Siigo** ERP integration for syncing accounting records

## Getting Started

### Prerequisites

- Go 1.25+
- PostgreSQL (optional — the server runs with an in-memory store without it)

### Environment Variables

Copy `.env.example` to `.env` and fill in the values:

| Variable | Description |
|---|---|
| `DATABASE_URL` | PostgreSQL connection string. If unset, uses in-memory store. |
| `ENCRYPTION_KEY` | 32-byte hex key used to encrypt Siigo credentials at rest. |
| `JWT_SECRET` | Secret key for signing JWT tokens. |
| `MS_CLIENT_ID` | Azure AD application (client) ID. |
| `MS_CLIENT_SECRET` | Azure AD client secret. |
| `MS_REDIRECT_URI` | OAuth callback URI (must match Azure AD app registration). |
| `MS_TENANT_ID` | Azure AD tenant ID. Leave empty to allow any Microsoft account. |
| `PORT` | Server port. Default: `8080`. |
| `FRONTEND_URL` | Frontend URL for OAuth redirects. Default: `http://localhost:5173`. |
| `ALLOWED_ORIGINS` | Comma-separated CORS origins. Default: `localhost:5173,localhost:3000`. |

### Run

```bash
go run ./cmd/server
```

## API Reference

All protected routes require a `Authorization: Bearer <token>` header.

Base path: `/api/v1`

### Auth

| Method | Path | Description |
|---|---|---|
| `POST` | `/auth/login` | Email/password login, returns JWT |
| `GET` | `/auth/microsoft` | Get Microsoft OAuth redirect URL |
| `GET` | `/auth/microsoft/callback` | Microsoft OAuth callback |
| `POST` | `/auth/logout` | Invalidate session |

### Transactions

| Method | Path | Description |
|---|---|---|
| `GET` | `/transactions` | List transactions (pagination, search, filters) |
| `POST` | `/transactions` | Create a transaction |
| `GET` | `/transactions/summary` | Total balance, monthly income and expenses |
| `GET` | `/transactions/{id}` | Get a single transaction |
| `PUT` | `/transactions/{id}` | Update a transaction |
| `DELETE` | `/transactions/{id}` | Delete a transaction |

> Transactions with `Source = "Siigo"` are read-only and cannot be edited or deleted.

### Dashboard & Reports

| Method | Path | Description |
|---|---|---|
| `GET` | `/dashboard` | Balance, charts, top expense categories, pending alerts |
| `GET` | `/cashflow` | Daily inflow/outflow, projected balance, pending alerts |
| `GET` | `/reports` | Financial reports by period (monthly, quarterly, annual) |
| `GET` | `/reports/export` | Export report data |

### Projections

| Method | Path | Description |
|---|---|---|
| `GET` | `/projections` | Projection data |
| `POST` | `/projections` | Create a projection |
| `POST` | `/projections/simulate` | Simulate cash flow with growth and payment delay parameters |

### Siigo Integration

| Method | Path | Description |
|---|---|---|
| `POST` | `/siigo/connect` | Connect with Siigo credentials |
| `GET` | `/siigo/status` | Check connection status |
| `POST` | `/siigo/sync` | Sync invoices and purchase orders by date range |

### Administration

| Method | Path | Description |
|---|---|---|
| `GET` / `POST` / `PUT` / `DELETE` | `/users` | User management |
| `GET` / `PUT` | `/settings` | App settings (currency, exchange rate) |
| `GET` / `POST` / `DELETE` | `/allowed-domains` | Email domain whitelist (admin only) |
| `GET` | `/categories` | List transaction categories |
| `GET` | `/activity-logs` | User activity log |

### Health

```
GET /health
```

## User Roles

| Role | Access |
|---|---|
| `ADMINISTRADOR` | Full access including user management and domain whitelist |
| `TESORERÍA` | Create, edit, and delete manual transactions |
| `CONSULTA` | Read-only access |

## Transaction Sources

| Source | Mutable |
|---|---|
| `Manual` | Yes — can be edited and deleted |
| `Siigo` | No — synced from ERP, read-only |
