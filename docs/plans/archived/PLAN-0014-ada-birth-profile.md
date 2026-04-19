# PLAN-0014 — Ada: Birth Profile (ada.born)

* **Status:** Complete
* **Date:** 2026-04-19
* **Project:** ruby-core
* **Roadmap Item:** none (standalone improvement)
* **Branch:** feat/ada-birth-profile
* **Related ADRs:** ADR-0029 (stateful processors)

---

## Scope

Handles the `ada.born` event fired by the HA title card when birth is
confirmed. Creates the `ada_profile` table via migration, persists Ada's
birth datetime, and makes it available as a future query boundary. The
table is explicitly excluded from any data clear operations and is
guaranteed to contain at most one row via a singleton constraint.

**Out of scope:** Any sensor pushes, UI changes beyond the `birth_at`
field fix (see Step 5 below), or data-clear logic.

---

## Corrections to the source brief (applied below)

### 1. Timezone ambiguity — resolved by sending RFC3339 from the browser

The original brief split the birth datetime into `dob` (YYYY-MM-DD) and
`time_of_birth` (HH:MM) and appended `Z` to construct a UTC timestamp.
This is wrong — the browser knows the user's local timezone; the engine
does not. The fix: the HA card constructs a full RFC3339 string in
`_confirmBirth()` before firing the event, and passes a single `birth_at`
field. The engine parses it with `time.Parse(time.RFC3339, d.BirthAt)` —
no timezone ambiguity. `AdaBornData` is updated accordingly.

### 2. ON CONFLICT idempotency — resolved by singleton constraint

`gen_random_uuid()` as PK means every insert gets a unique key and
`ON CONFLICT DO NOTHING` never triggers. The migration adds a
`singleton BOOLEAN NOT NULL DEFAULT true` column with a
`UNIQUE (singleton)` constraint. The query conflicts on `singleton`,
guaranteeing exactly one row with no application logic required.

### 3. No `Subscriptions()` change needed

`Subscriptions()` already returns `"ha.events.ada.>"`, which covers
`ha.events.ada.born`. Adding it explicitly would be a no-op.

---

## Pre-conditions

* [ ] On `main`, current with `v0.6.0`
* [ ] `go build ./...` passes clean
* [ ] `sqlc` available (`sqlc version` exits 0)
* [ ] HA effort deploys the `birth_at` RFC3339 field change before this
      is deployed (Step 5 below — coordinate with HA agent)

---

## Step 1 — Branch

**Action:** `git checkout -b feat/ada-birth-profile`

**Verification:** `git branch --show-current` returns `feat/ada-birth-profile`

---

## Step 2 — Add migration 000002

**Action:** Create
`services/engine/processors/ada/store/migrations/000002_ada_profile.up.sql`:

```sql
-- ada_profile stores Ada's birth information.
-- This table is explicitly excluded from any future data clear operations —
-- it is never truncated, soft-deleted in bulk, or affected by pre-birth
-- data cleanup.
--
-- The singleton constraint guarantees at most one row. ON CONFLICT (singleton)
-- DO NOTHING in the insert query makes the write idempotent — repeated
-- ada.born events (e.g. accidental double-tap) are silently ignored.
CREATE TABLE IF NOT EXISTS ada_profile (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    birth_at    TIMESTAMPTZ NOT NULL,
    singleton   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ada_profile_singleton UNIQUE (singleton)
);
```

Create
`services/engine/processors/ada/store/migrations/000002_ada_profile.down.sql`:

```sql
DROP TABLE IF EXISTS ada_profile;
```

**Verification:** Files exist. `go build ./...` passes clean (no code changes yet).

---

## Step 3 — Add profile queries and regenerate sqlc

**Action:** Create
`services/engine/processors/ada/store/queries/profile.sql`:

```sql
-- name: UpsertProfile :exec
-- Inserts the birth profile. ON CONFLICT (singleton) DO NOTHING makes this
-- idempotent — if ada.born fires more than once, the first write wins and
-- subsequent calls are silently ignored. birth_at is immutable once set.
INSERT INTO ada_profile (id, birth_at, singleton)
VALUES (gen_random_uuid(), @birth_at, true)
ON CONFLICT (singleton) DO NOTHING;

-- name: GetProfile :one
SELECT id, birth_at, created_at FROM ada_profile
LIMIT 1;
```

**Action:** `cd services/engine/processors/ada/store && sqlc generate`

**Verification:** `sqlc generate` exits 0. `GetProfile` and `UpsertProfile`
appear in the generated output. `go build ./...` passes clean.

---

## Step 4 — Add schema constant and payload struct

**Action:** In `pkg/schemas/ada.go`, append:

```go
const AdaEventBorn = "ha.events.ada.born"

// AdaBornData is the payload for the ada.born event fired when birth is confirmed.
// birth_at is a full RFC3339 timestamp constructed by the browser so that the
// user's local timezone is preserved — no server-side timezone inference needed.
type AdaBornData struct {
    BirthAt  string `json:"birth_at"`            // RFC3339, e.g. "2026-04-19T14:32:00-05:00"
    LoggedBy string `json:"logged_by,omitempty"`
}
```

**Verification:** `go build ./...` passes clean.

---

## Step 5 — Wire gateway routing

**Action:** In `services/gateway/ada/publish.go`, add to `eventRoutes`:

```go
"ada.born": schemas.AdaEventBorn,
```

**Verification:** `go build ./...` passes clean. The gateway will no longer
reject `ada.born` events with "unknown event type".

---

## Step 6 — Handle `ada.born` in the ada processor

**Action:** In `services/engine/processors/ada/processor.go`, add to
`ProcessEvent`'s switch:

```go
case schemas.AdaEventBorn:
    return p.handleBornEvent(ctx, evt)
```

Add handler, following the established `(ctx, evt schemas.CloudEvent)` pattern:

```go
func (p *Processor) handleBornEvent(ctx context.Context, evt schemas.CloudEvent) error {
    var d schemas.AdaBornData
    if err := remarshal(evt.Data, &d); err != nil {
        return fmt.Errorf("ada: decode born: %w", err)
    }

    birthAt, err := time.Parse(time.RFC3339, d.BirthAt)
    if err != nil {
        return fmt.Errorf("ada: parse birth_at %q: %w", d.BirthAt, err)
    }

    if err := p.q.UpsertProfile(ctx, toTimestamptz(birthAt)); err != nil {
        return fmt.Errorf("ada: upsert profile: %w", err)
    }

    p.log.Info("ada: birth profile saved",
        slog.String("birth_at", birthAt.UTC().Format(time.RFC3339)),
        slog.String("logged_by", d.LoggedBy),
    )
    return nil
}
```

**Verification:** `go build ./...` passes clean.
`go test -tags=fast -race ./services/engine/processors/ada/...` passes.

---

## Step 7 — Commit

**Action:** Stage and commit all changes.

```
feat: persist Ada birth profile from ada.born event
```

**Files:**

* `services/engine/processors/ada/store/migrations/000002_ada_profile.up.sql`
* `services/engine/processors/ada/store/migrations/000002_ada_profile.down.sql`
* `services/engine/processors/ada/store/queries/profile.sql`
* `services/engine/processors/ada/store/profile.sql.go` (sqlc generated)
* `services/engine/processors/ada/store/models.go` (sqlc regenerated)
* `services/engine/processors/ada/store/querier.go` (sqlc regenerated)
* `pkg/schemas/ada.go`
* `services/gateway/ada/publish.go`
* `services/engine/processors/ada/processor.go`

**Verification:** Pre-commit hooks pass. `go test -tags=fast -race ./...` green.

---

## Validation (post-deploy)

| Check | Command | Expected |
|---|---|---|
| Migration ran | `docker exec foundation-postgres psql -U ruby_core -d ruby_core -c "\dt"` | `ada_profile` present |
| Birth event processed | Trigger Confirm in HA dashboard | Engine logs `ada: birth profile saved` with correct `birth_at` |
| Profile persisted | `docker exec foundation-postgres psql -U ruby_core -d ruby_core -c "SELECT birth_at FROM ada_profile;"` | Row with correct datetime in UTC |
| Idempotent | Trigger Confirm a second time | No duplicate row; no error in engine logs |

---

## Rollback

Revert the commit and redeploy engine and gateway. Run down migration if
needed:

```bash
migrate -path services/engine/processors/ada/store/migrations \
  -database "postgres://ruby_core:<pw>@${FOUNDATION_HOST}:5432/ruby_core?sslmode=disable" \
  down 1
```

No other data affected. No HA schema changes.

---

## Note on future query boundary

`ada_profile.birth_at` is available to filter pre-birth test data from
any aggregate or history query without a schema migration:

```sql
AND timestamp >= (SELECT birth_at FROM ada_profile LIMIT 1)
```
