# Ruby Core v0 Roadmap

This document outlines the phased implementation plan for Ruby Core v0. The goal is not to build all features, but to prove the core architectural concepts and deliver a single, high-value automation use case from end to end.

## Phase 0: The Core Loop

**Outcome:** Prove the fundamental round-trip of an event from Home Assistant resulting in an action in Home Assistant, orchestrated by Ruby Core. This phase validates our core plumbing and integration strategy.

*   **Gateway: Ingress:**
    *   Connect to Home Assistant WebSocket.
    *   Subscribe to `state_changed` events.
    *   Normalize a subset of events into the internal Ruby Core event format.
    *   Publish normalized events to a NATS topic.
*   **Gateway: Egress:**
    *   Subscribe to a NATS `command.*` topic.
    *   Implement an HA REST client to execute a service call (e.g., `logbook.log`).
*   **Verification:**
    *   Manually publish a command to NATS and see it executed in HA.
    *   Verify HA events are visible on the NATS bus using a CLI client.

## Phase 1: The First Stateful Automation

**Outcome:** Implement a complete, stateful, and configurable automation. This phase tackles the core challenges of state management and dynamic rule definition, moving beyond simple, stateless event-reaction.

*   **Configuration:**
    *   Implement a mechanism to load automation definitions from local YAML files on startup.
*   **Engine:**
    *   Build the first `engine` service.
    *   Implement a simple, time-based stateful automation (e.g., "when a door opens, wait 10 seconds, then turn on a light").
*   **State Management:**
    *   Integrate a durable state store (e.g., NATS KV) to ensure the timer state survives service restarts.
*   **Verification:**
    *   Trigger the automation and restart the `engine` service mid-way through the delay.
    *   Confirm the automation completes successfully after the service comes back online.

## Phase 2: The First Specialized Engine

**Outcome:** Build the first specialized, high-value automation (reliable presence) by creating a dedicated, single-purpose engine. This demonstrates the "engine decomposition" strategy.

*   **Engine (`presence-engine`):**
    *   Create a new logical engine responsible only for presence.
    *   It will consume multiple inputs (e.g., UniFi device trackers, Bluetooth beacons, HA person states).
    *   It will manage a confidence-based state machine for each person.
*   **Events:**
    *   The `presence-engine` will publish new, high-order domain events (e.g., `person.entered.zone`, `person.departed.zone`, `person.confidence.changed`).
*   **Verification:**
    *   Show that fluttering, low-confidence presence events from underlying systems are successfully debounced into a single, stable `departed` event.

## Phase 3: Hardening & Observability

**Outcome:** Ensure the system is operationally mature, debuggable, and resilient before adding more feature complexity. This makes the system viable for "production" use in a real home.

*   **Observability:**
    *   Integrate OpenTelemetry across all services (`Gateway`, `engine`).
    *   Ensure trace context is propagated through NATS messages.
    *   Set up a local Jaeger/Zipkin instance to visualize traces.
*   **Health:**
    *   Implement the `Gateway` health heartbeat.
    *   Add health check endpoints to all services.
*   **Deployment:**
    *   Finalize the production Docker-compose files.
    *   Document the deployment and upgrade process.
*   **Verification:**
    *   Successfully trace a single automation flow from HA event ingress to HA command egress through the entire system.
    *   Manually kill the `Gateway` and confirm a `gateway.health` `disconnected` event is broadcast.
