# PLAN-0010 — Ada Backend Phase 2: Event Ingestion and Persistence

* **Status:** Approved
* **Date:** 2026-03-31
* **Project:** ruby-core
* **Roadmap Item:** none (pre-roadmap; tracked via docs/plans/)
* **Branch:** feat/ada-backend-phase2
* **Related ADRs:** ADR-0029 (stateful processors), ADR-0007 (logical processors), ADR-0015 (Vault secrets), ADR-0027 (subject naming)

---

## Scope

Delivers the full backend pipeline for the Ada baby tracking feature: a new `POST /ada/events`
gateway HTTP endpoint that publishes CloudEvents to `HA_EVENTS`, an extension of the engine to
support stateful processors with a conditional PostgreSQL boot step, and the `ada` processor
that persists feeding/diaper/sleep/tummy events to Postgres and pushes derived sensor state to
Home Assistant.

**Out of scope:** Home Assistant repo wiring (replacing `console.log` stubs with real HTTP calls,
removing hardcoded template sensors from `packages/ada.yaml`). That is Phase 3, in the HA repo.

---

## Pre-conditions

* [ ] Foundation Postgres running at `${FOUNDATION_HOST}:5432`, database `ruby_core`, user `ruby_core` (foundation-postgres init script confirms this)
* [ ] `secret/data/ruby-core/postgres` in Vault with fields: `host`, `port`, `dbname`, `user`, `password`
* [ ] `secret/data/ruby-core/ha` in Vault with fields: `url`, `token`
* [ ] `sqlc` CLI installed: `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`
* [ ] All existing pre-conditions (NATS running, Vault accessible, `go build ./...` passes clean)

---

## Context: What Was Verified Before Planning

The following was verified against the current codebase on 2026-03-31:

* `processor.Config` has **only** `RuleCfg`, `NC`, `JS` — no `Pool` or `HA` fields yet (step 6 adds them)
* `ProcessorHost.Initialize` signature is `(ruleCfg, nc, js)` — must be extended (step 7)
* `pkg/boot` has `FetchHAConfig` but **no** `FetchPostgresConfig` yet (step 5 adds it)
* `pkg/store` package does **not exist** (step 4 creates it)
* `services/gateway/ada/` does **not exist** (step 9 creates it)
* `services/engine/processors/ada/` does **not exist** (steps 10–11 create it)
* Gateway `app.go` `App` struct has no `nc` field and mux has no `/ada/events` route (step 9 adds both)
* `docs/adr/0029-stateful-processors.md` — **committed as part of this plan's Step 2**
* `go.mod` has no `pgx`, `golang-migrate` entries (step 3 adds them)
* Foundation Postgres: `foundation-postgres` container; init script creates `ruby_core` db + user

---

## Steps

### Step 1 — Branch

**Action:** `git checkout -b feat/ada-backend-phase2`

**Verification:** `git branch --show-current` returns `feat/ada-backend-phase2`

---

### Step 2 — Commit ADR-0029

**Action:** `docs/adr/0029-stateful-processors.md` was already created during planning. Stage and commit it:

```bash
git add docs/adr/0029-stateful-processors.md docs/plans/PLAN-0010-ada-backend-phase2.md
git commit -m "docs: add ADR-0029 stateful processors + PLAN-0010 ada backend phase2"
```

**Verification:** `ls docs/adr/0029-stateful-processors.md` exits 0. File contains "Accepted" in the status line.

---

### Step 3 — Add dependencies

**Action:**

```bash
go get github.com/jackc/pgx/v5
go get github.com/golang-migrate/migrate/v4
go get github.com/golang-migrate/migrate/v4/database/pgx/v5
go get github.com/golang-migrate/migrate/v4/source/iofs
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
```

**Verification:** `go.mod` contains `github.com/jackc/pgx/v5` and `github.com/golang-migrate/migrate/v4`. `sqlc version` prints a version string. `go build ./...` passes clean.

---

### Step 4 — Create `pkg/store` shared migration helper

**Action:** Create `pkg/store/migrate.go` with `MigrateUp(ctx, fs, dir, pool, tableName)` — the shared migration runner. Each processor passes its own embedded FS and a unique table name (e.g. `"schema_migrations_ada"`) to avoid collisions when multiple processors share the same database.

Key implementation notes:

* Use `iofs.New(fs, dir)` for the source
* Use `migratepgx.WithInstance(pool, &Config{MigrationsTable: tableName})` — pass `*pgxpool.Pool` directly, do not acquire a raw connection
* Treat `migrate.ErrNoChange` as success (idempotent — safe to call every startup)

**Verification:** `go build ./pkg/store/...` passes clean.

---

### Step 5 — Add `FetchPostgresConfig` to `pkg/boot`

**Action:** Add `PostgresConfig` struct and `FetchPostgresConfig(addr, token, path string)` to `pkg/boot/boot.go`, following the exact pattern of `FetchHAConfig` (same `withRetry` wrapper, same Vault KV v2 read, same error messages). Fields: `host`, `port`, `dbname`, `user`, `password`. Add `DSN()` method returning a pgx-compatible connection string (`sslmode=disable` — TLS handled at network layer).

Default path: `"secret/data/ruby-core/postgres"` (caller passes via env, empty string means use default).

**Verification:** `go build ./...` passes clean. `go test -tags=fast -race ./pkg/boot/...` passes.

---

### Step 6 — Extend `processor.Config` and add `StatefulProcessor` interface

**Action:** Update `services/engine/processor/processor.go`:

1. Add imports: `"github.com/jackc/pgx/v5/pgxpool"` and `"github.com/primaryrutabaga/ruby-core/pkg/boot"`
2. Add `Pool *pgxpool.Pool` and `HA *boot.HAConfig` fields to `Config` (with doc comments; nil when no stateful processors registered)
3. Add `StatefulProcessor` interface embedding `Processor` + `RequiresStorage() bool`

Update `services/engine/host.go` — add storage check in `Initialize` before calling `p.Initialize(cfg)`:

```go
if sp, ok := p.(processor.StatefulProcessor); ok && sp.RequiresStorage() {
    if cfg.Pool == nil {
        return fmt.Errorf("host: processor %T requires storage but Config.Pool is nil", p)
    }
}
```

**Verification:** `go build ./...` passes clean. `go test -tags=fast -race ./services/engine/...` passes.

---

### Step 7 — Extend engine `main.go` with conditional Postgres boot and update `ProcessorHost`

**Action:**

**In `host.go`:** Add `RequiresStorage() bool` method that returns true if any registered processor implements `StatefulProcessor` and returns true. Update `Initialize` signature to accept `pool *pgxpool.Pool` and `ha *boot.HAConfig`.

**In `main.go`:** Restructure the processor initialization section (currently: `host.Register(presence_notify.New(logger))` → `host.Initialize(ruleCfg, nc, js)`):

1. Register all processors first (including `ada.New(logger)` from step 11)
2. If `host.RequiresStorage()`:
   * Fetch Postgres config from Vault (`VAULT_PG_PATH` env, default `secret/data/ruby-core/postgres`)
   * Connect `pgxpool.Pool` — fatal on failure
   * Run `adastore.MigrateUp(ctx, pool)` — fatal on failure
   * Log "postgres: ada schema ready"
3. Fetch HA config from Vault (`VAULT_HA_PATH` env, default `secret/data/ruby-core/ha`) — fatal on failure
4. Call `host.Initialize(ruleCfg, nc, js, pool, haCfg)`

**In `host.go`:** Add the following comment block immediately above the `Initialize` method signature, so it is visible to any future implementer:

```go
// Initialize calls Initialize on every registered processor with the provided
// config and resources. pool and ha are passed through to Config and are non-nil
// only when at least one StatefulProcessor is registered (see RequiresStorage).
//
// Coupling note: HA config (ha) is currently fetched unconditionally whenever
// any stateful processor is registered, even if a given processor only needs
// Postgres and not HA. This is acceptable with a single stateful processor (ada).
// If a future stateful processor requires Postgres but not HA, this method should
// be extended to accept a richer options struct (or HA config should be fetched
// per-processor in Initialize rather than centrally here). Don't refactor until
// there is a second stateful processor to drive the design.
func (h *ProcessorHost) Initialize(...) error {
```

Import: `adastore "github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"`

Add `VAULT_PG_PATH=secret/data/ruby-core/postgres` and `VAULT_HA_PATH=secret/data/ruby-core/ha` to engine environment in `deploy/prod/compose.prod.yaml` and `deploy/dev/compose.dev.yaml`.

**Verification:** `go build ./...` passes clean. `go test -tags=fast -race ./services/engine/...` passes.

---

### Step 8 — Add Ada CloudEvent schemas

**Action:** Create `pkg/schemas/ada.go` with:

* Subject constants (`AdaEventFeedingEnded`, `AdaEventFeedingLogged`, `AdaEventFeedingSupplemented`, `AdaEventDiaperLogged`, `AdaEventSleepStarted`, `AdaEventSleepEnded`, `AdaEventSleepLogged`, `AdaEventTummyEnded`, `AdaEventTummyLogged`)
* Payload structs: `AdaFeedingEndedData`, `AdaFeedingSegment`, `AdaFeedingLoggedData`, `AdaFeedingSupplementData`, `AdaDiaperLoggedData`, `AdaSleepStartedData`, `AdaSleepEndedData`, `AdaSleepLoggedData`, `AdaTummyEndedData`, `AdaTummyLoggedData`

All amounts: `float64` in oz (no ml conversion performed here). Timestamps: RFC3339 `string`.

**Verification:** `go build ./...` passes clean.

---

### Step 9 — Gateway: add Ada HTTP handler

**Action:**

Create `services/gateway/ada/handler.go`:

* `Handler` struct with `nc *nats.Conn` and `log *slog.Logger`
* `New(nc, log) *Handler` constructor
* `ServeHTTP` implementation: POST only; decode JSON body; look up `event` field in `eventRoutes` map; build CloudEvent with `crypto/rand` ID; publish via `nc.Publish(subject, payload)`; return 202

`eventRoutes` maps frontend event type strings (e.g. `"ada.feeding.end"`) to NATS subjects (e.g. `schemas.AdaEventFeedingEnded`).

Update `services/gateway/app/app.go`:

* Add `nc *goNats.Conn` field to `App` struct
* In `New()`, store `nc` on the struct
* In `runHTTP()`, add: `mux.Handle("/ada/events", ada.New(a.nc, a.log))`

Note: `HA_EVENTS` already captures `ha.events.>` — no stream changes required.

**Verification:** `go build ./...` passes clean. `go test -tags=fast -race ./services/gateway/...` passes.

---

### Step 10 — Create ada processor store

**Action:** Create `services/engine/processors/ada/store/` with:

**Schema migrations:**

* `migrations/000001_initial_schema.up.sql` — tables: `feedings`, `feeding_bottle_detail`, `feeding_segments`, `diapers`, `sleep_sessions`, `tummy_time_sessions`, `ada_config`; partial indexes on timestamp columns (WHERE deleted_at IS NULL); soft-delete on all event tables
* `migrations/000001_initial_schema.down.sql` — drops all tables

**sqlc config:** `sqlc.yaml` with `engine: postgresql`, `sql_package: pgx/v5`, `emit_result_struct_pointers: true`, `emit_params_struct_pointers: true`

**Query files:**

* `queries/feedings.sql` — InsertFeeding, InsertFeedingBottleDetail, InsertFeedingSegment, GetLastFeeding, GetTodayFeedingAggregates
* `queries/diapers.sql` — InsertDiaper, GetLastDiaper, GetTodayDiaperAggregates
* `queries/sleep.sql` — InsertSleepStart, UpdateSleepEnd, InsertSleepSession, GetActiveSleepSession, GetLastSleepEnd, GetTodaySleepAggregates
* `queries/tummy.sql` — InsertTummySession, GetTodayTummyAggregates
* `queries/config.sql` — GetConfig, UpsertConfig

**Hand-written:** `store/migrate.go` with `//go:embed migrations/*.sql` and `MigrateUp(ctx, pool)` wrapping `pkgstore.MigrateUp` with `tableName="schema_migrations_ada"`.

Run `sqlc generate` from within `store/`. Commit all generated files (`db.go`, `models.go`, `querier.go`, plus any `*.sql.go` files).

**Verification:** `sqlc generate` exits 0. `go build ./...` passes clean.

---

### Step 11 — Create ada processor and HA client

**Action:**

Create `services/engine/processors/ada/ha/client.go`:

* `Client` struct wrapping `baseURL`, `token`, `http.Client`, `*slog.Logger`
* `NewClient(url, token string, log *slog.Logger) *Client`
* `PushState(ctx, entityID, state string, attributes map[string]any) error` — `POST /api/states/{entity_id}` with HA token header; log Warn on failure, do not return error to caller

Create `services/engine/processors/ada/processor.go` implementing `processor.StatefulProcessor`:

Key requirements:

* `RequiresStorage() bool { return true }`
* `Subscriptions()` returns `["ha.events.ada.>", "ha.events.input_number.ada_alert_threshold_h"]`
* `Initialize` sets up `store.Queries`, `adaha.Client`, bare `nc.Subscribe("gateway.health", ...)`, and calls `restoreSensors`
* `Shutdown` unsubscribes the bare health subscription (does NOT close pool or HA client — owned by engine)
* `ProcessEvent` unmarshals CloudEvent envelope, routes by `Type` field to per-event handlers
* `restoreSensors` queries all last-known state from Postgres and pushes to HA (see specification in plan brief)
* `handleHealthEvent` detects `false→true` transition on `ha_connected` and calls `restoreSensors`

**Timestamp rule (non-negotiable):** Every timestamp written to HA sensors MUST be the actual event time from the payload, never `time.Now()`. See event-type mapping in the pre-generated brief.

**Sensor push mapping by category:**

| Category | Sensors pushed |
| --- | --- |
| feeding | `ada_last_feeding_time`, `ada_last_feeding_source`, `ada_next_feeding_target`, `ada_today_feeding_count`, `ada_today_feeding_oz` |
| diaper | `ada_last_diaper_time`, `ada_last_diaper_type`, `ada_today_diaper_count`, `ada_today_diaper_wet`, `ada_today_diaper_dirty` |
| sleep_started | `ada_sleep_state`="sleeping", `ada_last_sleep_change` |
| sleep_ended | `ada_sleep_state`="awake", `ada_last_sleep_change`, `ada_today_sleep_hours`, `ada_today_sleep_nap_count` |
| tummy | `ada_today_tummy_time_min`, `ada_today_tummy_time_sessions` |
| threshold_change | `ada_next_feeding_target` (last_feeding_time + new_interval) |

Register in `main.go`: `host.Register(ada.New(logger))`

**Verification:** `go build ./...` passes clean. `go test -tags=fast -race ./services/engine/processors/ada/...` passes.

---

### Step 12 — Update engine compose files

**Action:** Add to engine service environment in both `deploy/prod/compose.prod.yaml` and `deploy/dev/compose.dev.yaml`:

```yaml
VAULT_PG_PATH: secret/data/ruby-core/postgres
VAULT_HA_PATH: secret/data/ruby-core/ha
```

No other compose changes — ada is part of the engine, not a new service.

**Verification:** `docker compose -f deploy/prod/compose.prod.yaml config` exits 0.

---

### Step 13 — Update smoke test

**Action:** Extend `scripts/smoke-test.sh` with an Ada Postgres round-trip check after the existing notifier check:

1. Publish a synthetic `ha.events.ada.diaper_logged` CloudEvent with `logged_by: "smoke"` using NATS admin credentials
2. Poll `foundation-postgres` container (psql) for up to 10s: `SELECT COUNT(*) FROM diapers WHERE logged_by = 'smoke' AND created_at > NOW() - INTERVAL '30 seconds'`
3. Pass if COUNT > 0, fail with non-zero exit otherwise
4. Clean up: `DELETE FROM diapers WHERE logged_by = 'smoke'`

Note: smoke check publishes directly to NATS (bypassing gateway HTTP). This isolates the engine → Postgres path. The full gateway → NATS path is validated by the manual curl in Step 15.

**Verification:** `bash -n scripts/smoke-test.sh` exits 0 (valid bash). Ada section is clearly delimited from the existing notifier check.

---

### Step 14 — Build and test

**Action:**

```bash
go build ./...
go test -tags=fast -race ./...
go test -tags=integration -race ./...
```

**Verification:** All pass. No new failures in existing engine, gateway, or pkg tests.

---

### Step 15 — [MANUAL] Deploy and validate end-to-end

**Action:** This step is performed by the user after the commit is pushed and deployed.

```bash
make prod-restart SERVICE=gateway
make prod-restart SERVICE=engine
```

**Validation checklist:**

| Check | Command | Expected |
| --- | --- | --- |
| Engine restarts cleanly | `make prod-logs SERVICE=engine` | "postgres: ada schema ready", "nats: pull consumer ready" — no fatal errors |
| Schema in Postgres | `docker exec -it foundation-postgres psql -U ruby_core -d ruby_core -c "\dt"` | 7 tables + `schema_migrations_ada` |
| Gateway accepts Ada event | `curl -X POST https://ruby-gateway.rutabagel.com/ada/events -H "Authorization: Bearer <HA_TOKEN>" -H "Content-Type: application/json" -d '{"event":"ada.diaper.log","type":"wet","timestamp":"2026-03-31T12:00:00Z"}'` | Returns 202 |
| Event in HA_EVENTS | `nats stream view HA_EVENTS --subject "ha.events.ada.>" --count 1` | Shows CloudEvent |
| Engine processes event | `make prod-logs SERVICE=engine` | Ada processor log line visible |
| Row in Postgres | `docker exec -it foundation-postgres psql -U ruby_core -d ruby_core -c "SELECT * FROM diapers ORDER BY created_at DESC LIMIT 1;"` | Shows wet diaper row |
| HA sensor updated | HA Developer Tools → States → `sensor.ada_last_diaper_time` | Shows RFC3339 timestamp |
| Run smoke test | `VAULT_TOKEN=... bash scripts/smoke-test.sh prod` | Both notifier and ada checks pass |

---

### Step 16 — Commit

**Action:** Single commit with message:

```
feat: extend engine with stateful processors; add ada baby tracking processor
```

**Files in commit:**

* `go.mod`, `go.sum`
* `pkg/boot/boot.go` (FetchPostgresConfig, PostgresConfig, DSN)
* `pkg/store/migrate.go`
* `pkg/schemas/ada.go`
* `services/engine/processor/processor.go` (Pool, HA fields; StatefulProcessor interface)
* `services/engine/host.go` (storage check, RequiresStorage, updated Initialize signature)
* `services/engine/main.go` (conditional Postgres boot, ada processor registration)
* `services/engine/processors/ada/processor.go`
* `services/engine/processors/ada/ha/client.go`
* `services/engine/processors/ada/store/migrations/000001_initial_schema.up.sql`
* `services/engine/processors/ada/store/migrations/000001_initial_schema.down.sql`
* `services/engine/processors/ada/store/queries/feedings.sql`
* `services/engine/processors/ada/store/queries/diapers.sql`
* `services/engine/processors/ada/store/queries/sleep.sql`
* `services/engine/processors/ada/store/queries/tummy.sql`
* `services/engine/processors/ada/store/queries/config.sql`
* `services/engine/processors/ada/store/sqlc.yaml`
* `services/engine/processors/ada/store/migrate.go`
* `services/engine/processors/ada/store/db.go` (sqlc generated)
* `services/engine/processors/ada/store/models.go` (sqlc generated)
* `services/engine/processors/ada/store/querier.go` (sqlc generated)
* `services/gateway/ada/handler.go`
* `services/gateway/app/app.go`
* `deploy/prod/compose.prod.yaml`
* `deploy/dev/compose.dev.yaml`
* `scripts/smoke-test.sh`

---

## Rollback

All changes are additive or extend existing files:

* **Gateway Ada handler:** Remove `/ada/events` route from `app.go`, delete `services/gateway/ada/`. Redeploy gateway.
* **Engine:** Remove `ada.New(logger)` registration from `main.go`, remove Postgres boot block, revert `processor.Config` and `host.go` changes. Redeploy engine. Postgres connection is no longer established.
* **Postgres schema:** `migrate -path services/engine/processors/ada/store/migrations -database "postgres://ruby_core:<pw>@${FOUNDATION_HOST}:5432/ruby_core?sslmode=disable" down 1`
* **Source:** `git revert` the commit.

No existing services removed. No existing processors modified. Full rollback available at any point.
