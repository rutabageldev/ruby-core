# engine

Automation rules engine. Consumes the `HA_EVENTS` stream (`ha.events.>`) and the `PRESENCE` stream (`ruby_presence.events.>`), evaluates YAML-defined rules (`configs/rules/*.yaml`), and publishes command events to the `COMMANDS` stream and fused presence events to the `PRESENCE` stream.

On startup the engine:

- Ensures all JetStream streams exist: `HA_EVENTS`, `DLQ`, `AUDIT_EVENTS`, `COMMANDS`, `PRESENCE`
- Publishes compiled config (passlist, critical entities) to the `config` NATS KV bucket for the gateway to consume
- Initialises the idempotency deduplication store (hybrid memory + NATS KV, 24h TTL)
- If any registered processor requires storage (ADR-0029): fetches Postgres credentials from Vault, runs schema migrations, connects a connection pool

## Processors

| Processor | Stateful | Subscriptions |
|---|---|---|
| `presence_notify` | No | `ha.events.>`, `ruby_presence.events.>` |
| `ada` | Yes (Postgres) | `ha.events.ada.>`, `ha.events.input_number.ada_alert_threshold_h` |

The `ada` processor persists feeding, diaper, sleep, and tummy time events to PostgreSQL and pushes derived sensor state to Home Assistant after each event. It also subscribes to the bare `gateway.health` subject to restore HA sensor state after a gateway reconnect. A background ticker runs every 60 seconds to push `sensor.ada_sleep_session_min` while a session is active, refresh daily aggregates at midnight rollover, and perform a full sensor restore every 4 hours as a safety net against HA state loss.

Four sensors carry a 24-hour rolling history array as their `entries[]` attribute: `sensor.ada_feeding_history`, `sensor.ada_diaper_history`, `sensor.ada_sleep_history`, and `sensor.ada_tummy_history`. Each is pushed after the relevant event and on every daily restore. Sensor state is the entry count; active sleep sessions appear in `sensor.ada_sleep_history` with `end_time` and `duration_s` omitted. The `last_*` sensors (e.g. `sensor.ada_last_diaper_time`, `sensor.ada_last_sleep_change`) reflect the chronologically newest event by timestamp, so back-dating an older event does not overwrite them.

When a caregiver claims a due feed (`ada.feeding.claimed`), the engine owns the claim lifecycle: it sets `input_boolean.ada_feeding_claimed` on and projects the claimer to `sensor.ada_feeding_claimed_by`, then clears both when the next feed is completed.

The `ada.born` event persists Ada's birth datetime to the singleton `ada_profile` table and marks the engine "born". While not yet born the engine forces `test=true` on every event so all pre-birth data is selectable as test data regardless of the dashboard toggle. The engine does **not** wipe at birth — the clean slate is performed by the host **`ada-birth-watcher`** (ADR-0036), which `pg_dump`s the database before clearing the pre-birth (`test=true`) data and restarting the engine. A re-fired `ada.born` is a no-op.

Recorded events can be corrected or removed via `ada.{feeding,diaper,sleep,tummy,growth}.update` (full-resolution replacement) and `ada.{...}.delete {id}` (soft-delete via `deleted_at`), each recomputing the derived sensors. Every row carries a `test BOOLEAN` marker (ADR-0031): test data behaves identically in every projection but is selectable for bulk teardown by the seed/clear tooling — see [docs/runbooks/ada-test-data.md](../../docs/runbooks/ada-test-data.md).

The Trends view is served by request/response (ADR-0032): the dashboard fires `ada.trends.query {metric, view, period, request_id[, offset]}`; the engine computes calendar-anchored buckets at local midnight (week = Sun–Sat 7×1-day, month = Sun–Sat weeks clipped to the calendar month, 4–6 buckets, year = 12 calendar months), navigable via `offset ≤ 0` (0 = current period, −n = n periods back), and publishes the result — `{request_id, metric, view, period, generated_at, buckets, totals, grand, prevGrand, window_start, window_end, days_elapsed, offset, min_offset}` — to `sensor.ada_trends`, echoing the `request_id` so the dashboard renders only its latest request. The current period always includes today (future buckets zero-filled); `prevGrand` compares the immediately prior period, truncated to `days_elapsed` for the current partial one; `min_offset` bounds navigation at the period containing Ada's DOB. Note trends deliberately use calendar midnight rather than the Today view's bedtime rollover (ADR-0032 §5 vs ADR-0043), so an evening feed counts toward the trends bucket of the calendar day it happened on even though the Today view has already rolled past it — the two will not agree on a partial current day, by design.

A bottle's volume is stored across two columns that are each incomplete on their own: a single-source bottle carries `amount_oz` with an empty split, a mixed bottle logged via `ada.feeding.log` carries the split with `amount_oz = 0`. Both the `feeding`/`bottle` trend and `sensor.ada_today_feeding_oz` therefore take the greater of `amount_oz` and `breast_milk_oz + formula_oz`, attributing any excess to a seg by feed source (ADR-0032 §4 amendment). A residual with no attributable source is logged, not dropped silently.

**Medications & Emergency** (ROADMAP-0011, ADR-0037/0038). The engine persists the medication registry (`ada.medication.upsert/delete`), dosing routines (`ada.medication.routine.upsert/delete`), dose events (`ada.medication.given/skipped`, with an immutable `dose_amount`/`dose_unit` snapshot; `ada.medication.event.update/delete`), as-needed watches (`ada.medication.series.start/end`), and the emergency card (`ada.emergency.row.upsert/delete`, `ada.emergency.reorder`). It projects `sensor.ada_medications` (registry + the per-med guard: `last_given`, `earliest_safe`, `doses_in_24h`), `sensor.ada_med_routines` (with `next_due` on interval routines), `sensor.ada_med_events` (a 7-day dose history plus active watches in its `series` attribute), and `sensor.ada_emergency_card` (ordered rows). Registry, routines, and emergency rows are standing config (`test` from the event only — they survive the birth clean-slate); dose events are tracking (pre-birth-forced). ids are dashboard-provided strings (not UUIDs), and `medication_id` is a loose ref (no FK) because events arrive and are processed concurrently. On the 60-second tick the engine is authoritative (ADR-0038) for the time-edge transitions that must hold with the app closed: it emits actorless `missed` doses by supersession (never stacking), auto-completes routines (without writing a phantom dose), expires watches past a 24-hour backstop, and pushes a "Medication due" notification to active caretakers — the only medication state it writes without a caregiver action.

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `VAULT_ADDR` | `http://127.0.0.1:8200` | Vault server address |
| `VAULT_TOKEN` | *(required)* | Read-only token scoped to `secret/ruby-core/*` |
| `VAULT_NKEY_PATH` | `secret/data/ruby-core/nats/engine` | NATS NKEY seed |
| `VAULT_TLS_PATH` | `secret/data/ruby-core/tls/engine` | NATS mTLS cert, key, CA |
| `VAULT_PG_PATH` | `secret/data/ruby-core/postgres` | Postgres credentials (host, port, dbname, user, password) — only read if a stateful processor is registered |
| `VAULT_HA_PATH` | `secret/data/ruby-core/ha` | HA base URL and access token — only read if a stateful processor is registered |
| `HA_INGEST_ENABLED` | *(unset → enabled)* | Set to `false` to disable HA projection. All environments share one HA, so only prod projects Ada sensors — non-prod engines set this `false` and their HA pushes become no-ops (ADR-0033). |
| `NATS_URL` | `tls://localhost:4222` | NATS server URL |
| `NATS_REQUIRE_MTLS` | `false` | Force mTLS even if NATS_URL is not `tls://` |
| `RULES_DIR` | `configs/rules` | Directory of `*.yaml` rule files |
| `ENVIRONMENT` | *(unset)* | Set to `production` to enforce HTTPS Vault |
| `VAULT_ALLOW_HTTP` | `false` | Override HTTPS enforcement for co-located Vault |
| `ENGINE_FORCE_FAIL` | `false` | **Test hook only.** Forces all events to NAK → DLQ. Never set in production. |

## Health check

No HTTP endpoint. Liveness is inferred from NATS pull consumer activity in the logs.

## Known failure modes

**No rule files found or invalid schema** — exits 1 at boot. Ensure `configs/rules/*.yaml` is present and mounted correctly in the container; all files must declare `schemaVersion: v1`.

**Stream or KV setup failure** — exits 1. Usually indicates NATS is unavailable or the service NKEY lacks the necessary JetStream permissions.

**`ENGINE_FORCE_FAIL=true`** — all events are rejected and routed to the DLQ. This is a deliberate test hook for DLQ verification (see `docs/ops/phase3-verification.md`). If you see this in production logs, the variable was set unintentionally.

**Postgres unreachable or credentials missing at boot** — exits 1 if any stateful processor is registered and the Postgres connection cannot be established. Check that `secret/data/ruby-core/postgres` exists in Vault and that `foundation-postgres` is reachable from the engine container.
