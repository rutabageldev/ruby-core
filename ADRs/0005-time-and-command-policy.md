# ADR-0005 - Adopt a Hybrid Time-to-Live (TTL) Policy for Commands

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

In a control plane for physical devices, executing stale commands can be unpredictable and unsafe (e.g., unlocking a door minutes after the request was relevant). Furthermore, for time-based automations to be reliable, all services must have a consistent understanding and usage of time. This ADR defines a strict policy for both command expiry and time semantics.

## Decision

We will adopt a hybrid, "defense in depth" TTL policy for all commands, and a strict "event time" semantic for all business logic.

1. **Time Semantics Convention:** For any business logic involving time, all services **MUST** use the `time` attribute from the CloudEvents envelope (`event time`). The local server time (`processing time`) should only be used for calculating metrics (e.g., processing latency) or for validating command expiry as described below.

2. **Hybrid TTL Enforcement:**
    * **Broker-Level TTL (Primary Defense):** All command messages **SHOULD** be published with a `Nats-Msg-Expires` header. The value must be a UTC timestamp in a format NATS understands (e.g., RFC3339). This is the efficient, first line of defense that allows the broker to automatically discard expired messages.
    * **Application-Level TTL (Safety Net):** All command messages **MUST** contain a `valid_until` field in their application payload. This field **MUST** be an absolute UTC timestamp in RFC3339 format.

3. **Consumer Validation Rule:** Before execution, the final consuming service **MUST** validate the command. A command is considered invalid and **MUST** be rejected if:
    * The `valid_until` field is missing, malformed, or empty.
    * The consumer's local clock, expressed in UTC, is past the `valid_until` timestamp. (A small, acceptable clock skew should be noted in implementation guidance, but the check is mandatory).

## Consequences

### Positive Consequences

* **Safety & Reliability:** The two-layer TTL approach provides a robust defense against stale commands, which is critical for a system controlling the physical world.
* **Explicit Contract:** The mandatory `valid_until` field makes the command's intended lifecycle an explicit, self-documenting part of its application-level contract.
* **Improved Debuggability:** Consistent use of `event time` for all business logic makes reasoning about and debugging time-based sequences of events far more reliable.
* **Infrastructure Resilience:** The application-level check provides a safety net against misconfiguration of broker-level expiry rules.

### Negative Consequences

* **Implementation Overhead:** Requires discipline from developers to consistently set the `Nats-Msg-Expires` header on publish and to perform the `valid_until` check on consumption.
* **Minor Performance Cost:** Involves the minor overhead of serializing an extra timestamp and performing a time comparison check.

### Neutral Consequences

* The primary TTL mechanism creates a dependency on the `Nats-Msg-Expires` feature of NATS JetStream. The application-level TTL mitigates the risk of this dependency.
