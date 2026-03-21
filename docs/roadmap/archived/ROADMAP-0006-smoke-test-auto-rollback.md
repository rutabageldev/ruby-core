# Post-Deploy Smoke Test & Auto-Rollback

* **Status:** Complete
* **Date:** 2026-03-03
* **Project:** ruby-core
* **Related ADRs:** none
* **Linked Plan:** none

---

**Goal:** Eliminate silent broken deploys by running a full end-to-end pipeline check immediately after every `make deploy-prod`, rolling back automatically on failure and pushing a phone notification either way.

---

## Efforts

1. Write `scripts/smoke-test.sh $VERSION` — publishes a synthetic command event to the COMMANDS stream, then polls `AUDIT_EVENTS` for the expected `audit.ruby_notifier.notification_sent` confirmation within a timeout. Accepts an optional `ROLLBACK_FROM` env var so the notification can read "vX.X.X failed, rollback to vY.Y.Y was successful".
2. Extend `deploy-prod` in the Makefile to: (a) capture the currently-running version before pulling (written to `.last-deployed-version`); (b) run `smoke-test.sh $VERSION` after the NATS SIGHUP; (c) on smoke test failure, re-deploy the previous version and re-run the smoke test as rollback validation, then exit non-zero.
3. On smoke test **pass**: push HA notification "Deployment of ruby-core vX.X.X successful at HH:MM" to `mobile_app_phone_michael`.
4. On smoke test **fail + rollback pass**: push "ruby-core vX.X.X failed — rollback to vY.Y.Y successful at HH:MM".
5. On smoke test **fail + rollback fail**: push "ruby-core vX.X.X failed — rollback to vY.Y.Y also failed. Manual intervention required." and exit non-zero loudly.

---

## Done When

`make deploy-prod` sends a push notification on every deploy outcome, automatically rolls back broken deploys, and exits non-zero on double failure.

---

## Acceptance Criteria

* `[X]` A successful `make deploy-prod` sends a push notification to Michael's phone confirming the version and time.
* `[X]` Deploying a broken image (e.g. bad ACLs, missing rule) triggers automatic rollback and a failure notification.
* `[X]` `make deploy-prod` exits non-zero if rollback is also required, making CI-friendliness possible in Phase 8.
