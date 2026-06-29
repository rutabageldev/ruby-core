# PLAN-0037 - Production NATS-Resilience Hardening

* **Status:** In Progress
* **Date:** 2026-06-29
* **Project:** ruby-core
* **Roadmap Item:** none (resilience / drift cleanup)
* **Branch:** fix/nats-resilience
* **Related ADRs:** ADR-0039 (NATS boot-time recovery — adjacent), ADR-0021/0022 (NATS/DLQ)

---

## Scope

One PR hardening how services behave during a NATS restart, a Vault/NATS outage, and a prod
deploy. Closes #18 (engine consume loop bails on a NATS bounce and the process hangs alive-but-
dead), #111 (cold NATS/Vault → instant exit → respawn storm), #61 (deploy starts services before
reloading NATS's CA), and #62 (already fixed by #45/#50 — close). Out of scope: re-ensuring a
*deleted* durable consumer in-loop (covered by the reconnect→exit→restart→re-ensure path); the
broader OTEL/metrics work (#31/#137).

---

## Pre-conditions

* [x] nats.go reconnects by default (MaxReconnects=60); #18 is "our loop exited, nobody consumes",
      not "nats.go didn't reconnect". The fix is to keep looping so it resumes post-reconnect.
* [x] All 5 NATS services (gateway, engine, notifier, presence, audit-sink) connect via
      `boot.BootstrapNATSTLS` → `ConnectNATS`/`ConnectNATSDynamicTLS`, and share the SIGTERM shape
      `cancel()` then `defer nc.Close()`. `api`/`adapters` use no NATS.
* [x] Loops that EXIT on a fetch error (the #18 bug): engine `Consumer.Run`, audit-sink
      `runFetchLoop`. notifier/presence already `continue` but with no sleep (latent hot-spin).

---

## Steps

### Step 1 — Commit this plan

**Verification:** pre-commit passes; committed on the branch + indexed in plans/README (active).

### Step 2 — Shared reconnect opts + retried dial (#111) — `pkg/boot/boot.go`, `pkg/boot/pki.go`

`resilienceOpts(slog.Default())` → `MaxReconnects(60)` (**finite**, so the ClosedHandler can fire;
not -1), `ReconnectWait(2s)` + `ReconnectJitter`, Disconnect/Reconnect log handlers. Append in both
`ConnectNATS` and `ConnectNATSDynamicTLS`. **No `RetryOnFailedConnect`** (it masks a down server and
breaks the later `nc.JetStream()`). Wrap the `nats.Connect` call in `withRetryLabeled("nats", …)`
(a labeled sibling of `withRetry` so logs aren't mislabeled `vault:`).
**Verification:** `go build ./...`; unit test `withRetryLabeled` retries then returns last error.

### Step 3 — Consume loops survive a bounce (#18) — engine `consumer.go`, audit-sink, notifier, presence

Replace the fatal error arm with ctx-aware backoff + continue (exit only on `ctx.Err()`); add
`isTransientFetchErr` (pure, unit-tested). The mandatory `select{<-ctx.Done(); <-time.After(backoff)}`
sleep prevents hot-spin (a disconnected Fetch returns immediately). Add the sleep to notifier/presence too.
**Verification:** `go test -tags=fast ./services/engine/... ./services/audit-sink/...`; isTransientFetchErr table test green.

### Step 4 — ClosedHandler → clean exit (#18) — all 5 mains

`onNATSClosed(ctx, cancel, &natsLost, log)` (pure, unit-tested); each main `nc.SetClosedHandler(...)`
after connect; `ctx.Err()!=nil` early-return distinguishes graceful shutdown from outage. Engine:
`cancel()` unblocks dlqFwd → `wg.Wait()` returns; add `if natsLost.Load() { os.Exit(1) }` (same in
all 5 mains).
**Verification:** unit test onNATSClosed (cancelled→no-op; live→sets flag+cancels); all services build + existing tests pass.

### Step 5 — deploy-prod.sh reorder (#61)

`pull` → `up -d nats-init nats nats-cert-renewer` → force-recreate `nats-cert-renewer` → `docker wait
…nats-init` → `SIGHUP …-nats` → `up -d` (rest). Mirror in the **rollback branch** too.
**Verification:** shellcheck clean; the SIGHUP/reload precedes the dependent-service `up -d` in both branches.

### Step 6 — Integration test + close #62 + README

Extend `pkg/natsx/consumer_integration_test.go`: connect with reconnect enabled; **pause/unpause** the
NATS container (not stop/start — port remap) → assert the loop survives the blip and resumes. Fix the
stale `nohup setsid` docstrings (`stability-watch.sh`, `release.yml`) and `gh issue close 62`. Update
`services/gateway/README.md` ("exits 1 immediately" → retries with backoff).
**Verification:** `make test-integration` green; #62 closed.

### Step 7 — Pre-Push + archive

Full `go test -tags=fast ./...` + `golangci-lint run ./...` + integration; archive this plan; notify.

---

## Rollback

Revert the merge — no schema/data. Per-service code + deploy-script changes are revert-safe.
Blast radius: NATS connection behavior changes for all 5 services; the integration + manual NATS
bounce/outage checks gate it.

---

## Open Questions

None. PR description must call out the corrected scope (4 loops, 5 mains) and the known limitation
(deleted durable consumer → relies on restart path, not in-loop re-ensure).
