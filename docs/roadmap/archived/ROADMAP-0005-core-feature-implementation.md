# Core Feature Implementation

* **Status:** Complete
* **Date:** 2026-03-02
* **Project:** ruby-core
* **Related ADRs:** ADR-0006, ADR-0007, ADR-0008, ADR-0009, ADR-0020
* **Linked Plan:** none

---

**Goal:** Build the primary business logic of the `gateway` and `engine` services.

---

## Efforts

1. **Edge Auth:** Configure Traefik with middleware to handle edge authentication before any API endpoints are exposed (ADR-0020).
2. **Gateway:** Implement the full HA WebSocket client, lean projection, health heartbeat, and targeted reconciliation logic (ADR-0009, ADR-0008).
3. **Engine:** Implement the "Logical Processor" framework and the YAML configuration file loader (ADR-0007, ADR-0006).
4. Implement one complete, real automation (presence detection for Katie).

---

## Done When

The gateway connects to HA and processes events, all exposed APIs are behind Traefik auth, and the engine loads a YAML rule and executes a real automation end-to-end.

---

## Acceptance Criteria

* `[X]` Any exposed API on the `gateway` is protected by Traefik.
* `[X]` The `gateway` can connect to HA, process events, and reconcile state.
* `[X]` The `engine` can load a YAML rule and execute a simple automation.

---

## Implementation Notes

* Presence service: `services/presence/` — fuses phone + WiFi state, publishes to PRESENCE stream
* Engine PRESENCE consumer: `engine_presence_processor` on PRESENCE stream
* Rule files: `configs/rules/katie_presence.yaml`
* Verification report: `docs/ops/phase5-verification.md`
