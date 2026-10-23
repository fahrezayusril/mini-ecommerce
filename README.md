# Mini e-commerce REST API

[![CI](https://github.com/fahrezayusril/mini-ecommerce/actions/workflows/ci.yml/badge.svg?branch=develop)](https://github.com/fahrezayusril/mini-ecommerce/actions/workflows/ci.yml)

A small e-commerce backend written in Go on top of PostgreSQL. It covers user registration and login with JWT, a product catalog that admins manage, and a checkout that deducts stock inside a single database transaction. I built it as a backend assessment, and I tried to keep it small enough to read in one sitting while still handling the parts that usually go wrong: money, concurrency, and access control.

- Live API: https://mini-ecommerce-production.onrender.com
- Interactive docs: https://mini-ecommerce-production.onrender.com/docs
- Health check: https://mini-ecommerce-production.onrender.com/health
- Source: https://github.com/fahrezayusril/mini-ecommerce

## Live demo

The API runs on a free Render web service with a free Neon PostgreSQL database.

| What | URL |
|---|---|
| Base URL | `https://mini-ecommerce-production.onrender.com` |
| Swagger UI | `https://mini-ecommerce-production.onrender.com/docs` |
| Raw OpenAPI spec | `https://mini-ecommerce-production.onrender.com/docs/openapi.yaml` |
| Health | `https://mini-ecommerce-production.onrender.com/health` |

The free Render instance goes to sleep after 15 minutes without traffic. The first request after that wakes it up and can take about a minute, so give `/health` a moment before assuming something is broken. A healthy instance answers `200 {"status":"ok","database":"ok"}`; if the database ping fails it answers `503 {"status":"degraded","database":"unreachable"}`.

A quick tour from the terminal (needs `curl` and `jq`):

```bash
BASE=https://mini-ecommerce-production.onrender.com

curl -s $BASE/health

TOKEN=$(curl -s -X POST $BASE/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"customer@gmail.com","password":"gakngerti"}' | jq -r '.data.token')

curl -s "$BASE/api/v1/products?in_stock=true&sort=price&order=asc&limit=3"

curl -s $BASE/api/v1/orders -H "Authorization: Bearer $TOKEN"
```

In Swagger UI, log in with one of the accounts below, copy the `token` from the response, and paste it into **Authorize**. The token survives a page reload.

## Test credentials

| Role | Email | Password |
|---|---|---|
| admin | `admin@gmail.com` | `gakngerti` |
| customer | `customer@gmail.com` | `gakngerti` |

Both accounts are created by the seeder on startup from the `SEED_ADMIN_*` and `SEED_CUSTOMER_*` environment variables. The seeder only inserts a user when no user with that email exists, so restarts and redeploys do not duplicate them. New accounts made through `/auth/register` are always customers.

## Tech stack

- Go 1.26: one static binary, a good standard library for HTTP, and goroutines make the concurrency tests easy to write.
- chi v5: a thin router on top of `net/http` with route groups and per-route middleware, nothing more.
- PostgreSQL 17 (Neon in production): real transactions, row locks, partial indexes, and CHECK constraints, all of which the checkout relies on.
- pgx v5 with `pgxpool`: a fast native driver; every query is hand-written SQL with `$1`, `$2` parameters. No ORM.
- golang-migrate: plain `.up.sql` / `.down.sql` files, embedded into the binary and applied on startup.
- google/uuid: UUID v7 primary keys created in Go.
- shopspring/decimal: exact decimal math for prices and totals instead of floats.
- golang-jwt v5: HS256 tokens with `sub`, `role`, `iat`, and `exp`.
- bcrypt (`golang.org/x/crypto`): password hashing with the default cost.
- go-playground/validator v10: request body validation through struct tags.
- testify: assertions in unit tests and in integration tests against a real PostgreSQL.
- Docker: a two-stage build that ends in `gcr.io/distroless/static-debian12:nonroot`.
- GitHub Actions, GHCR, Render, Neon: CI on every pull request, and an image-based deploy from the `production` branch.

## Architecture

The code follows a plain layered layout: handlers parse HTTP and shape JSON, services hold the business rules, repositories hold the SQL. Services depend on small interfaces rather than concrete repositories, which is what lets the service tests run without a database.

### Request path

```mermaid
flowchart LR
    client([Client]) --> router[chi router]
    router --> logger[chi Logger]
    logger --> recover[Recover]
    recover --> cors[CORS]
    cors --> public{Protected route?}
    public -- no --> handler
    public -- yes --> authn[Authenticate<br/>verify JWT]
    authn --> role[RequireRole<br/>admin or customer]
    role --> handler[Handler<br/>decode, validate, map errors]
    handler --> service[Service<br/>business rules]
    service --> repo[Repository<br/>hand-written SQL]
    repo --> db[(PostgreSQL)]
```

`Recover` turns a panic into a `500 INTERNAL_ERROR` response and logs the stack trace. `Authenticate` only checks the bearer token; role checks happen in `RequireRole`, which is attached per route (for example `POST /orders` requires `customer`, `PATCH /orders/{id}/status` requires `admin`).

### Delivery path

```mermaid
flowchart LR
    dev[feature branch] -->|pull request| develop
    develop -->|pull request| staging
    staging -->|pull request| production
    develop & staging & production -. push and PR .-> ci[CI workflow<br/>lint, test, docker-build]
    production -->|push| deploy[Deploy workflow]
    deploy --> checks[Reuse CI jobs]
    checks --> ghcr[(GHCR<br/>sha-short and production tags)]
    ghcr --> hook[Render deploy hook<br/>imgURL = sha-short tag]
    hook --> render[Render web service<br/>pulls the image]
    render --> neon[(Neon PostgreSQL)]
    hook --> smoke[Smoke test GET /health<br/>up to 5 minutes]
```

### Folder layout

```
cmd/api/                main.go (startup, graceful shutdown) and the `healthcheck` subcommand
api/                    openapi.yaml and spec.go, which embeds it with //go:embed
migrations/             SQL migrations, embedded with //go:embed
internal/config         environment loading and validation
internal/database       pgxpool setup with retries, migration runner, transaction helper, seeder
internal/domain         entities, filters, and domain errors
internal/auth           JWT issuing and parsing, password hashing, email normalization
internal/repository     SQL for users, products, orders
internal/service        auth, product, and order business logic
internal/handler        HTTP handlers, request and response DTOs, validation, docs page
internal/httpx          JSON response envelopes and error codes
internal/middleware     JWT authentication, role check, panic recovery
internal/server         router wiring and dependency setup
```

## Database

Four tables, created by the files in `migrations/`. This diagram matches them column for column.

```mermaid
erDiagram
    users ||--o{ orders : "places"
    orders ||--o{ order_items : "contains"
    products ||--o{ order_items : "is bought as"

    users {
        UUID id PK
        VARCHAR name "100, not null"
        VARCHAR email "255, not null, unique on lower(email)"
        VARCHAR password_hash "255, not null"
        VARCHAR role "20, CHECK admin or customer"
        TIMESTAMPTZ created_at "default now()"
        TIMESTAMPTZ updated_at "default now()"
    }

    products {
        UUID id PK
        VARCHAR name "200, not null"
        TEXT description "not null, default empty string"
        NUMERIC price "12,2, CHECK price >= 0"
        VARCHAR category "100, not null"
        INT stock "CHECK stock >= 0"
        TIMESTAMPTZ created_at "default now()"
        TIMESTAMPTZ updated_at "default now()"
        TIMESTAMPTZ deleted_at "nullable, soft delete"
    }

    orders {
        UUID id PK
        UUID user_id FK "users.id, ON DELETE RESTRICT"
        NUMERIC total_amount "12,2, CHECK total_amount >= 0"
        VARCHAR status "20, default PENDING, CHECK PENDING COMPLETED CANCELLED"
        TIMESTAMPTZ created_at "default now()"
        TIMESTAMPTZ updated_at "default now()"
    }

    order_items {
        UUID id PK
        UUID order_id FK "orders.id, ON DELETE CASCADE"
        UUID product_id FK "products.id, ON DELETE RESTRICT"
        VARCHAR product_name "200, snapshot at purchase"
        INT quantity "CHECK quantity > 0"
        NUMERIC unit_price "12,2, CHECK unit_price >= 0"
        NUMERIC subtotal "12,2, CHECK subtotal = unit_price * quantity"
    }
```

Some notes on the choices behind it.

UUID v7 everywhere. Every `id` column is `UUID PRIMARY KEY` with no database default; the value comes from `uuid.NewV7()` in Go. V7 ids start with a timestamp, so new rows land near the end of the primary key index instead of at random positions, and because the database never generates them, the schema works on any PostgreSQL version without extensions. The seeder test checks that stored ids really are version 7.

Money is `NUMERIC(12,2)` in the database and `decimal.Decimal` in Go, never a float. JSON responses render it as a number with exactly two decimals (`389500.00`). Inputs with more than two decimal places are rejected, and the largest accepted amount is `9999999999.99`, which is the most `NUMERIC(12,2)` can hold. That limit also applies to an order total, so a huge cart fails with a 400 instead of a database overflow.

Products are soft deleted. `DELETE /products/{id}` sets `deleted_at` and every read filters on `deleted_at IS NULL`. Old orders still point at the product row through `order_items.product_id`, and that foreign key is `ON DELETE RESTRICT`, so a hard delete would either fail or force me to drop history.

Order items keep a snapshot. `product_name` and `unit_price` are copied from the product at checkout. If an admin renames a product or changes its price later, past orders still show what the customer actually paid for.

CHECK constraints back up the application rules: non-negative prices and stock, a closed list of roles and statuses, `quantity > 0`, and `subtotal = unit_price * quantity`. The service already enforces these, but the constraints mean a bug in my Go code cannot write a negative stock or an inconsistent line item. `TestCheckConstraints` tries each violation directly in SQL.

Indexes follow the queries:

- `users_email_lower_key`: unique index on `lower(email)`, so `John@x.com` and `john@x.com` cannot both register.
- `products_category_lower_active_idx`, `products_price_active_idx`, `products_created_at_active_idx`: partial indexes `WHERE deleted_at IS NULL` on `lower(category)`, `price`, and `created_at`, matching the catalog filters and sorts while skipping deleted rows.
- `orders_user_id_created_at_idx` on `(user_id, created_at DESC)` for a customer's order history, and `orders_created_at_idx` on `(created_at DESC)` for the admin view.
- `order_items_order_id_idx` on `(order_id)` for loading items of a page of orders.

Migrations are embedded into the binary and applied with golang-migrate every time the app starts, before the HTTP server listens. There is no separate migration step to forget during a deploy.

## How the main flows work

### Auth

`POST /api/v1/auth/register` takes `{name, email, password}`. The name is trimmed and must be 1 to 100 characters, the email must be valid, and the password must be 8 to 72 bytes (72 is bcrypt's limit). The email is trimmed and lowercased before it is stored, the password is hashed with bcrypt, and the role is always `customer`. A duplicate email, in any letter case, comes back as `409 EMAIL_ALREADY_EXISTS`; the repository detects it from the unique index violation, so two concurrent registrations cannot both win.

`POST /api/v1/auth/login` takes `{email, password}`. A wrong password and an unknown email both return `401 INVALID_CREDENTIALS` with the same message. For an unknown email the service still runs a bcrypt comparison against a dummy hash, so the two cases take about the same time and the response timing does not reveal which emails are registered.

Both endpoints return the same shape:

```json
{"data": {"user": {"id": "...", "name": "...", "email": "...", "role": "customer"}, "token": "...", "expires_at": "2026-10-24T08:11:48Z"}}
```

The token is HS256 with `sub` (user id), `role`, `iat`, and `exp` (24 hours by default, `JWT_TTL`). The middleware rejects tokens signed with any other algorithm, tokens without `exp`, and tokens whose role is not `admin` or `customer`.

### Product listing

`GET /api/v1/products` supports `page`, `limit`, `category`, `min_price`, `max_price`, `in_stock`, `sort`, and `order`. Every filter value becomes a bound parameter (`lower(category) = lower($1)`, `price >= $2`, and so on). `sort` and `order` cannot be bound as parameters in SQL, so the handler maps them through a whitelist (`price` or `created_at`, `asc` or `desc`) and anything else is a `400 VALIDATION_ERROR` before the repository is reached. The repository maps the result through a second whitelist to the column name. `TestProductsAPIRejectsSQLInjectionAttempts` sends injection strings through `sort`, `order`, `page`, `min_price`, and `category`, and checks the product count afterwards.

The `ORDER BY` always adds `id` as a tie-breaker in the same direction, so products with the same price or the same `created_at` (all seeded products share one) do not shuffle between pages. The total count runs with the same `WHERE` clause as the page query, so `meta.total` matches the filters.

### Checkout

`POST /api/v1/orders` takes `{"items": [{"product_id": "...", "quantity": 2}]}` with 1 to 50 entries and a quantity of at least 1. Only customers can place orders; an admin token gets `403 FORBIDDEN`.

```mermaid
sequenceDiagram
    autonumber
    actor C as Customer
    participant H as OrderHandler
    participant S as OrderService
    participant DB as PostgreSQL

    C->>H: POST /api/v1/orders
    H->>H: decode JSON, validate items
    H->>S: Checkout(userID, items)
    S->>S: merge duplicate product_ids, sort by id
    S->>DB: BEGIN
    S->>DB: SELECT id, name, price, stock FROM products<br/>WHERE id = ANY($1) AND deleted_at IS NULL<br/>ORDER BY id FOR UPDATE
    DB-->>S: locked product rows
    alt a product is missing or deleted
        S->>DB: ROLLBACK
        S-->>H: 404 PRODUCT_NOT_FOUND
    else stock is short for any item
        S->>DB: ROLLBACK
        S-->>H: 409 INSUFFICIENT_STOCK with details
    else everything is available
        S->>S: unit_price and subtotals from DB prices, total
        S->>DB: UPDATE products SET stock = stock - qty (unnest)
        S->>DB: INSERT INTO orders (status PENDING)
        S->>DB: INSERT INTO order_items (unnest)
        S->>DB: COMMIT
        S-->>H: order
        H-->>C: 201 {"data": order}
    end
```

The client only sends ids and quantities. Prices, subtotals, and the total come from the locked product rows, so a client cannot set its own price. When several items are short, the error lists all of them, not just the first:

```json
{"error": {"code": "INSUFFICIENT_STOCK", "message": "Insufficient stock for one or more products",
  "details": [{"product_id": "01a167d0-c1ae-72ad-807a-65675ae298b0", "requested": 100000, "available": 58}]}}
```

Any error inside the transaction, including one from the final insert, rolls everything back. `TestCheckoutRollsBackWritesOnLateFailure` checks that stock is untouched when the order insert fails after the stock update already ran.

### Order status and cancellation

`PATCH /api/v1/orders/{id}/status` is admin only and accepts `COMPLETED` or `CANCELLED`. The only allowed transitions are from `PENDING`; anything else, such as cancelling a completed order or cancelling twice, returns `409 INVALID_STATUS_TRANSITION` with the current and requested status in `details`.

Cancelling gives the stock back, in one transaction: lock the order row with `SELECT ... FOR UPDATE`, check the transition, sum the item quantities per product, lock those product rows `ORDER BY id FOR UPDATE`, add the quantities back, and update the status. Because the order row is locked first, two admins cancelling the same order at the same time cannot both restore stock: the second one waits for the lock, then sees `CANCELLED` and gets the 409. `TestCancelRestoresStockOnce` checks that stock comes back exactly once (including for a product that was soft deleted after the order) and that a second cancel or a later complete is rejected.

Order reads are scoped by role. A customer's `GET /orders` only returns their own orders, and `GET /orders/{id}` for someone else's order returns `404 NOT_FOUND`, the same as a missing order, so ids of other people's orders cannot be probed. Admins see every order, and each order in an admin response also carries `user: {id, name, email}`.

## Consistency under concurrency

The checkout takes row locks with `SELECT ... FOR UPDATE` on every product in the cart, and it takes them in a fixed order: the service sorts the merged items by UUID, and the query itself says `ORDER BY id FOR UPDATE`. Cancellation locks products the same way.

The order matters because of deadlocks. If one customer buys `[A, B]` and another buys `[B, A]` at the same moment, and each transaction locks rows in the order the client sent them, the first holds A and waits for B while the second holds B and waits for A. PostgreSQL detects that cycle and kills one of them. When every transaction locks in ascending id order, the second one simply waits for A and then proceeds.

Holding the lock between reading the stock and writing it is what prevents overselling. Two buyers cannot both read "5 left" and both subtract, because the second `SELECT ... FOR UPDATE` blocks until the first transaction commits, and then reads the updated stock.

Two integration tests in `internal/repository/order_repository_integration_test.go` prove this against a real PostgreSQL:

- `TestCheckoutConcurrentBuyersNeverOversell`: a product with stock 5, and 20 goroutines released at the same moment, each buying 1. Exactly 5 checkouts succeed, exactly 15 fail with `INSUFFICIENT_STOCK` (requested 1, available 0), the stock ends at 0, and only 5 orders exist.
- `TestCheckoutOppositeItemOrderDoesNotDeadlock`: 30 concurrent checkouts, half sending `[A, B]` and half `[B, A]`. All succeed, no `40P01 deadlock_detected` error appears, and both stocks drop by exactly 30.

The `stock >= 0` CHECK constraint is the last safety net under all of this.

## API overview

All business endpoints live under `/api/v1`, take and return JSON, and use `Authorization: Bearer <token>` where a token is needed.

| Method | Path | Access | Success |
|---|---|---|---|
| GET | `/health` | public | 200 (503 if the database is unreachable) |
| GET | `/docs` | public | 200, Swagger UI |
| GET | `/docs/openapi.yaml` | public | 200, raw spec |
| POST | `/api/v1/auth/register` | public | 201 |
| POST | `/api/v1/auth/login` | public | 200 |
| GET | `/api/v1/products` | public | 200 |
| GET | `/api/v1/products/{id}` | public | 200 |
| POST | `/api/v1/products` | admin | 201 |
| PUT | `/api/v1/products/{id}` | admin | 200 |
| DELETE | `/api/v1/products/{id}` | admin | 204 |
| POST | `/api/v1/orders` | customer | 201 |
| GET | `/api/v1/orders` | customer (own) or admin (all) | 200 |
| GET | `/api/v1/orders/{id}` | owner or admin | 200 |
| PATCH | `/api/v1/orders/{id}/status` | admin | 200 |

`GET /docs/` with a trailing slash redirects to `/docs` with a 301.

Responses use three shapes:

```json
{"data": {...}}
{"data": [...], "meta": {"page": 1, "limit": 10, "total": 42, "total_pages": 5}}
{"error": {"code": "VALIDATION_ERROR", "message": "Request validation failed", "details": [{"field": "sort", "message": "must be one of price, created_at"}]}}
```

`details` is only present when there is something specific to report: field errors for validation, missing product ids, stock shortages, or the status pair for a rejected transition.

| HTTP | Code | When |
|---|---|---|
| 400 | `VALIDATION_ERROR` | a field, query parameter, or path id fails validation |
| 400 | `INVALID_JSON` | malformed body, empty body, unknown field, wrong type, or more than 1 MB |
| 401 | `UNAUTHORIZED` | missing, malformed, invalid, or expired bearer token |
| 401 | `INVALID_CREDENTIALS` | wrong email or password at login |
| 403 | `FORBIDDEN` | valid token, wrong role |
| 404 | `NOT_FOUND` | product or order not found, or not visible to this customer; also unknown routes |
| 404 | `PRODUCT_NOT_FOUND` | a checkout item points at a missing or deleted product |
| 405 | `NOT_FOUND` | known route, unsupported method |
| 409 | `EMAIL_ALREADY_EXISTS` | registering an email that is already taken |
| 409 | `INSUFFICIENT_STOCK` | at least one checkout item exceeds available stock |
| 409 | `INVALID_STATUS_TRANSITION` | status change not starting from `PENDING` |
| 500 | `INTERNAL_ERROR` | anything unexpected; the message is generic and details only go to the log |

The full contract, with every parameter, schema, and example, is in `/docs` (source: `api/openapi.yaml`). The test `TestEveryRouteIsDocumentedAndEverySpecOperationIsRouted` walks the chi router and the spec and fails if either has an operation the other does not.

## Running locally

### Prerequisites

- Docker with the Compose plugin
- Go 1.26 (only for running outside Docker and for tests)
- `curl` and `jq` for the examples below

### With Docker Compose

```bash
git clone https://github.com/fahrezayusril/mini-ecommerce.git
cd mini-ecommerce
cp .env.example .env
docker compose up --build
```

This starts PostgreSQL 17 and the API on `http://localhost:8080`. The API waits for the database health check, applies migrations, seeds the two users and 10 products (because `.env.example` sets `SEED_ON_START=true`), and starts listening. In the logs you should see:

```
database migrations applied
database seeded: admin_created=true customer_created=true products_created=10
http server listening on :8080
```

With the example values the local accounts are `admin@example.com` and `customer@example.com`, both with password `change-me-please`.

Stop the stack with `docker compose down`. Add `-v` to also delete the database volume.

### With the Go CLI

Run only the database in Docker and the API on the host:

```bash
cp .env.example .env
docker compose up -d db
go run ./cmd/api
```

The app loads `.env` from the working directory on startup, and the `DATABASE_URL` in `.env.example` already points at `localhost:5432`. Inside Compose the `api` service overrides `DATABASE_URL` to reach the `db` container instead, so the same `.env` works for both.

### Trying it out

These are the requests I ran against a fresh local stack. The same commands work against the live API if you change `BASE` and use the credentials from the test credentials section.

```bash
BASE=http://localhost:8080

curl -s $BASE/health
# 200 {"status":"ok","database":"ok"}

TOKEN=$(curl -s -X POST $BASE/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"customer@example.com","password":"change-me-please"}' | jq -r '.data.token')
ADMIN_TOKEN=$(curl -s -X POST $BASE/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"change-me-please"}' | jq -r '.data.token')

curl -s "$BASE/api/v1/products?category=books&sort=price&order=asc&limit=2"
# 200, two books cheapest first, meta.total = 3

PRODUCT_ID=$(curl -s "$BASE/api/v1/products?in_stock=true&limit=1" | jq -r '.data[0].id')

curl -s -X POST $BASE/api/v1/orders -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":2}]}"
# 201, status PENDING, total_amount = 2 x price

curl -s -X POST $BASE/api/v1/orders -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":100000}]}"
# 409 INSUFFICIENT_STOCK with requested and available

ORDER_ID=$(curl -s "$BASE/api/v1/orders?status=PENDING" -H "Authorization: Bearer $ADMIN_TOKEN" | jq -r '.data[0].id')

curl -s -X PATCH $BASE/api/v1/orders/$ORDER_ID/status -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"status":"CANCELLED"}'
# 200, status CANCELLED, stock of the product goes back up by 2

curl -s -X PATCH $BASE/api/v1/orders/$ORDER_ID/status -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"status":"COMPLETED"}'
# 409 INVALID_STATUS_TRANSITION

curl -s -X POST $BASE/api/v1/products -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"USB-C Cable","description":"1m braided cable","price":59000,"category":"Electronics","stock":40}'
# 201, price rendered as 59000.00
```

### Environment variables

| Variable | Required | Default | Notes |
|---|---|---|---|
| `PORT` | no | `8080` | 1 to 65535 |
| `DATABASE_URL` | yes | | PostgreSQL connection string |
| `JWT_SECRET` | yes | | at least 32 characters |
| `JWT_TTL` | no | `24h` | any positive Go duration |
| `SEED_ON_START` | no | `false` | seed users and products after migrations |
| `SEED_ADMIN_NAME` | when seeding | | 1 to 100 characters |
| `SEED_ADMIN_EMAIL` | when seeding | | must contain `@`, different from the customer email |
| `SEED_ADMIN_PASSWORD` | when seeding | | 8 to 72 characters |
| `SEED_CUSTOMER_NAME` | when seeding | | 1 to 100 characters |
| `SEED_CUSTOMER_EMAIL` | when seeding | | must contain `@` |
| `SEED_CUSTOMER_PASSWORD` | when seeding | | 8 to 72 characters |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | Compose only | | used by the `db` service and to build the API's `DATABASE_URL` inside Compose |
| `POSTGRES_PORT` | Compose only | `5432` | host port for the database |
| `TEST_DATABASE_URL` | tests only | | integration tests are skipped when it is empty |

If anything is missing or invalid, the app stops at startup and lists every problem at once, for example `invalid configuration: JWT_SECRET must be at least 32 characters`.

## Tests

There are two kinds of tests, and both use the standard `testing` package with testify.

Unit tests need nothing but Go:

```bash
go test ./...
```

They cover config loading and validation, JWT issuing and parsing (wrong secret, `none` and HS512 algorithms, expired, missing `exp`, bad subject, unknown role), password hashing, request validation and error mapping in every handler, the JSON shape of responses (money with two decimals, empty lists as `[]`), the auth and role middleware, panic recovery, the order service rules (merging items, building totals from database prices, collecting every shortage, allowed transitions, role scoping), the access matrix of the router, and the check that the router and the OpenAPI spec match. Integration tests are in the same packages, but they call `t.Skip` when `TEST_DATABASE_URL` is not set, so this command does not need a database.

Integration tests run against a real PostgreSQL. With the Compose database running and a `.env` in place, the Makefile target creates a separate `mini_ecommerce_test` database and runs them:

```bash
docker compose up -d db
make itest
```

Or, step by step, the same thing CI runs (with the race detector):

```bash
docker compose up -d db
docker compose exec -T db sh -c 'createdb -U "$POSTGRES_USER" mini_ecommerce_test'
TEST_DATABASE_URL='postgres://app:change-me@localhost:5432/mini_ecommerce_test?sslmode=disable' \
  go test -p 1 ./... -race -count=1
```

The URL above matches the values in `.env.example`; adjust it if you changed them. `-p 1` matters: each integration test drops and re-applies the migrations on the shared test database, so test packages must not run in parallel against it.

What the integration tests cover:

- `internal/database`: migrations create all tables, run down and up again cleanly, and are a no-op when re-applied; seeding is idempotent and case-insensitive on email, stores bcrypt hashes and v7 ids, and creates 10 products in 3 to 4 categories with at least one out of stock; every CHECK constraint rejects bad rows; the transaction helper rolls back on error.
- `internal/repository`: product filters, sorting in both directions, stable pagination, counts that match the filters, soft delete; checkout totals and stock deduction, rollback on shortage and on a late failure, missing and deleted products, the two concurrency tests described above, cancel restoring stock exactly once, completed orders staying final, and order visibility per role.
- `internal/server`: the full HTTP stack on a real database, including the product admin lifecycle, the seeded catalog, SQL injection attempts on the list query parameters, and an end-to-end order flow.

## CI/CD

There are three long-lived branches: `develop` (the default), `staging`, and `production`. Work goes into a feature branch, then moves `develop` to `staging` to `production`, each step through a pull request that I merge myself. Branch protection requires the three CI checks to pass. `main` is not used.

### `ci.yml`

Runs on pushes and pull requests to the three branches, on manual dispatch, and as a reusable workflow (`workflow_call`). It has `contents: read` permission only and caches Go modules. The job names are stable because branch protection refers to them:

- `lint`: fails if `gofmt -l .` lists any file, runs `go vet ./...`, and lints `api/openapi.yaml` with `@redocly/cli`.
- `test`: starts a `postgres:17-alpine` service container, sets every required environment variable to a dummy value (with `SEED_ON_START=false`), and runs `go test -p 1 ./... -race -count=1`, so the integration tests run here too.
- `docker-build`: builds the `prod` stage of the Dockerfile for `linux/amd64` without pushing, with a GitHub Actions layer cache.

### `deploy.yml`

Runs on pushes to `production` and on manual dispatch. It belongs to the `production` GitHub environment and uses a concurrency group per branch with `cancel-in-progress: false`, so a deploy is never cut off halfway.

1. `checks` calls `ci.yml`, so production gets exactly the same lint, test, and build checks.
2. `deploy` logs in to GHCR with `GITHUB_TOKEN` (`packages: write`), then builds and pushes `ghcr.io/fahrezayusril/mini-ecommerce` for `linux/amd64` with two tags, `sha-<short sha>` and `production`, and OCI labels including `org.opencontainers.image.source`.
3. It calls the Render deploy hook from the `RENDER_DEPLOY_HOOK_URL` secret and appends a URL-encoded `imgURL` pointing at the `sha-<short sha>` tag, so Render deploys exactly the image that was just built. If the secret is empty, the step prints a notice and skips.
4. It polls `${PRODUCTION_BASE_URL}/health` every 10 seconds for up to 5 minutes and fails the job if it never gets a 200. The long window is there for the free instance's cold start. If the variable is empty, the step is skipped with a notice.

On the Render side, the web service is configured to run the image `ghcr.io/fahrezayusril/mini-ecommerce:production`, with its environment variables (including the Neon `DATABASE_URL`) set in the Render dashboard. On boot the container applies any new migrations against Neon and seeds if needed, then starts serving.

## Design decisions and trade-offs

Hand-written SQL instead of an ORM. Every query is a string I can read and run in `psql`, and the checkout depends on things an ORM tends to hide: `FOR UPDATE`, lock order, `unnest` for batch updates, and partial indexes. The cost is more boilerplate for scanning rows and building the dynamic `WHERE` clause, and no compile-time check that a column exists. The integration tests cover the gap.

UUID v7 created in Go. Ids do not reveal how many users or orders exist, and unlike random v4 ids they stay roughly in insert order, which is kinder to the B-tree index. Generating them in the application avoids depending on a PostgreSQL version or extension for `uuidv7()`. The trade-off is 16-byte keys instead of 8, ids that are not human friendly, and a timestamp prefix that does reveal when a row was created.

`NUMERIC` and `decimal.Decimal` for money. Floats cannot represent `0.10` exactly, and errors add up across line items. The price of exactness is a custom JSON type to print two decimals and some care parsing numeric input. The 12-digit precision caps a single amount at just under ten billion, which is plenty for the sample catalog but would need widening for larger amounts.

Soft delete for products. Deleting a product keeps its row, so order history and the `RESTRICT` foreign key stay intact, and a mistake can be undone in SQL. The cost is that every product query has to remember `deleted_at IS NULL`, and the indexes are partial to match. Deleted products also keep their name, so nothing stops an admin from creating a new product with the same name.

Price and name snapshots on `order_items`. An order is a record of what happened, so it should not change when the catalog does. This duplicates a little data per line item. I kept `product_id` as well, so it is still possible to join back to the current product.

Pessimistic locks in id order. I considered an atomic `UPDATE products SET stock = stock - $1 WHERE id = $2 AND stock >= $1` per item, and optimistic locking with a version column. Both work for a single item, but the checkout needs to report every shortage at once with the available amounts, and needs prices from the same consistent snapshot it deducts from. `SELECT ... FOR UPDATE` gives that in one query. The cost is that buyers of the same product queue behind each other for the length of a short transaction, which is fine at this scale.

`404` for someone else's order. Returning 403 would confirm that the order id exists. The trade-off is a slightly less precise error for a legitimate mistake.

Same response and similar timing for both login failures. This makes it harder to find out which emails have accounts. Registration still reveals it through `409 EMAIL_ALREADY_EXISTS`, which the API contract requires, so this only closes one of the two doors.

A hand-written OpenAPI file served at `/docs` instead of swaggo. swaggo builds the spec from annotation comments on handlers, and I wanted the code free of those. Instead `api/openapi.yaml` is written by hand, embedded with `//go:embed`, and served next to a Swagger UI page that loads its assets from jsDelivr with pinned versions and SRI hashes. The risk of a hand-written spec drifting from the code is handled by a test that compares routes to spec operations, by Redocly lint in CI, and by a test that the served file is byte-identical to the one in the repo. The UI does depend on a CDN being reachable.

Migrations and seeding at startup. A fresh database, local or on Neon, becomes usable with nothing but `docker compose up` or a deploy. Seeding runs in one transaction behind `pg_advisory_xact_lock`, so two instances starting together cannot seed twice. The trade-off is that a bad migration stops the app from booting rather than failing in a separate step, and with several replicas each one would try to migrate on start (golang-migrate locks so only one applies).

Startup also retries the first database connection up to 10 times, 2 seconds apart. That covers a Compose `db` container that is still starting and a free Neon database that is waking up.

Render and Neon on free tiers, deployed from an image. Building the image in GitHub Actions and pushing it to GHCR means the exact artifact that passed CI is the one that runs, and Render only has to pull it. Pinning the deploy to the `sha-` tag makes it clear which commit is live. Neon is used through a direct connection, not its pooler: golang-migrate takes a session-level advisory lock, and pgx caches prepared statements per connection, and both assume a real session, which a transaction-mode pooler does not give. Free tiers mean cold starts, a sleeping web service, and no SLA, which is acceptable for a demo but not for real traffic.

## Known limitations and what I would do next

- No refresh tokens and no way to revoke a token. There is no logout either: a token stays valid until it expires (24 hours by default). Next I would add short-lived access tokens with a refresh token stored server side.
- No rate limiting. Login and register can be hammered. I would add a per-IP limiter on the auth routes first.
- The demo credentials are public in this README, so anyone can log in as the admin on the live instance and change the catalog. That is deliberate for review, and it is why the live database holds nothing but sample data.
- Cold starts on the free tier. The first request after 15 idle minutes can take about a minute.
- The gRPC bonus from the assessment is not implemented.
- Stock is reserved as soon as an order is `PENDING`, and there is no payment step or timeout. An order that is never completed or cancelled holds its stock until an admin cancels it. A real shop would expire unpaid orders automatically.
- No idempotency key on `POST /orders`, so a client that retries after a network timeout can place the same order twice.
- Pagination is offset based. It is simple and gives `total_pages`, but deep pages get slower and rows can shift between pages while new orders arrive. Cursor pagination on `(created_at, id)` would fix both.
- CORS allows any `http://` or `https://` origin. That keeps the API easy to call from anywhere during review, but a real deployment should list its front-end origins.
- Admin accounts can only be created by the seeder. There is no endpoint to promote a user or create another admin.
- Logging is chi's plain request logger plus `log.Printf`. Structured JSON logs with a request id, and basic metrics, would be the first observability step.
