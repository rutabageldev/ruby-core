# Changelog

## [0.29.0](https://github.com/rutabageldev/ruby-core/compare/v0.28.0...v0.29.0) (2026-06-29)


### Features

* **observability:** distributed traces — W3C propagation + per-service spans (PLAN-0009 PR 2/2) ([5861b74](https://github.com/rutabageldev/ruby-core/commit/5861b7472d1b203b1acf72031e7863ca917e1647))
* **otel:** gateway ha.ingest span + W3C inject on publish paths (PLAN-0009 traces 2/3) ([7a1b092](https://github.com/rutabageldev/ruby-core/commit/7a1b092a038c080ba2e86aa74eca0bda90236de7))
* **otel:** NATS trace propagation + engine ctx threading (PLAN-0009 traces 1/3) ([d4171b3](https://github.com/rutabageldev/ruby-core/commit/d4171b32652bc21d2a3e33328cf9409013d69717))
* **otel:** notify.send + presence.wifi_check leaf spans (PLAN-0009 traces 3/3) ([b65f98b](https://github.com/rutabageldev/ruby-core/commit/b65f98bd1d6e7faa9fd72a807e68d569d219e65e))


### Bug Fixes

* **lint:** suppress gosec G704 on configured HA REST calls (PLAN-0009 traces) ([ff740e9](https://github.com/rutabageldev/ruby-core/commit/ff740e98dec9b71628108d61e059267abadd296b))

## [0.28.0](https://github.com/rutabageldev/ruby-core/compare/v0.27.1...v0.28.0) (2026-06-29)


### Features

* **deploy:** wire app services to OTLP collector via observability network (PLAN-0009 Step 9) ([e35cf60](https://github.com/rutabageldev/ruby-core/commit/e35cf60db1ed147cee8dd23c469ad5e45f37f795))
* **logging:** inject trace_id/span_id into stdout JSON logs (PLAN-0009 Step 3) ([f69411d](https://github.com/rutabageldev/ruby-core/commit/f69411dec3fe6f34c0c05a542b4a62601a3690e6))
* **observability:** OTLP metrics + log-trace correlation (PLAN-0009 PR 1/2) ([230c4fe](https://github.com/rutabageldev/ruby-core/commit/230c4fecfabd7ed31fd4325e6776f9e80a0300c9))
* **otel:** instrument consumer loops with OTLP metrics + wire otel.Init (PLAN-0009 Steps 4-7) ([54d83e3](https://github.com/rutabageldev/ruby-core/commit/54d83e32ab14f8896b1da990ef4643ad44a48483))
* **otel:** migrate ada counter to OTLP + idempotency metrics ([#137](https://github.com/rutabageldev/ruby-core/issues/137), ADR-0004) ([6b64613](https://github.com/rutabageldev/ruby-core/commit/6b646136bb4493d870e84f15e3906a3362ed1c8f))
* **otel:** per-service domain counters (PLAN-0009 Step 8, metrics-only) ([4a9ffc6](https://github.com/rutabageldev/ruby-core/commit/4a9ffc6e79cff39985ce538df5cb5edb10678a22))
* **otel:** pkg/otel.Init — OTLP gRPC trace+metric providers + W3C propagator (PLAN-0009 Steps 1-2) ([ac78cda](https://github.com/rutabageldev/ruby-core/commit/ac78cdae4953eb7f414f35ffb2836846c2f0ce2e))

## [0.27.1](https://github.com/rutabageldev/ruby-core/compare/v0.27.0...v0.27.1) (2026-06-29)


### Bug Fixes

* **deploy:** reload NATS CA before starting app services ([#61](https://github.com/rutabageldev/ruby-core/issues/61)) ([b170887](https://github.com/rutabageldev/ruby-core/commit/b17088710d1fcb16b6544d645a47f3cef7ef4322))
* production NATS-resilience — survive bounces, retry cold dial, clean deploy ordering ([c60e7cd](https://github.com/rutabageldev/ruby-core/commit/c60e7cd3d81fb1e1d43d7365e0bd547588d971db))
* **services:** survive NATS bounces + retry cold dial, exit cleanly on permanent loss ([#18](https://github.com/rutabageldev/ruby-core/issues/18), [#111](https://github.com/rutabageldev/ruby-core/issues/111)) ([ad172b8](https://github.com/rutabageldev/ruby-core/commit/ad172b86eb65da865f12106b9b1747c32772a9be))

## [0.27.0](https://github.com/rutabageldev/ruby-core/compare/v0.26.2...v0.27.0) (2026-06-29)


### Features

* **calendar:** constrain relationship + multi-email attendee reconciliation ([#134](https://github.com/rutabageldev/ruby-core/issues/134), [#133](https://github.com/rutabageldev/ruby-core/issues/133)) ([25fc6d4](https://github.com/rutabageldev/ruby-core/commit/25fc6d422213f02bf8823e62ea3cce239dda9c12))


### Bug Fixes

* **gateway:** derive stable idempotency_key for calendar upserts ([#138](https://github.com/rutabageldev/ruby-core/issues/138)) ([b901c6e](https://github.com/rutabageldev/ruby-core/commit/b901c6e3dd77c05ce88b1fc1939bf0ae938b6727))

## [0.26.2](https://github.com/rutabageldev/ruby-core/compare/v0.26.1...v0.26.2) (2026-06-28)


### Bug Fixes

* **calendar:** quote etag in If-Match header ([#140](https://github.com/rutabageldev/ruby-core/issues/140)) ([44f643d](https://github.com/rutabageldev/ruby-core/commit/44f643d075c2610275baca417489efe5dadea02e))
* **calendar:** quote etag in If-Match header so modify stops 412'ing ([#140](https://github.com/rutabageldev/ruby-core/issues/140)) ([933c8cf](https://github.com/rutabageldev/ruby-core/commit/933c8cf1544f76116d2d0fb3c5c493933c6141eb))

## [0.26.1](https://github.com/rutabageldev/ruby-core/compare/v0.26.0...v0.26.1) (2026-06-28)


### Bug Fixes

* **calendar:** idempotent write-through at Google + shrink dedup TTL (PLAN-0034) ([088b76c](https://github.com/rutabageldev/ruby-core/commit/088b76c72b2081c6aa04797cffb11665db18173e))
* **calendar:** make write-through idempotent at Google + shrink dedup TTL ([6defb1b](https://github.com/rutabageldev/ruby-core/commit/6defb1ba5438c797f4f70aae28dbbecc71d9bb5f))

## [0.26.0](https://github.com/rutabageldev/ruby-core/compare/v0.25.1...v0.26.0) (2026-06-28)


### Features

* **api:** activate api deploy + provisioning runbook (Slice D) ([c39ba8e](https://github.com/rutabageldev/ruby-core/commit/c39ba8efb63eab8634e50032b4c69872f56f78fc))
* **overlay:** household overlay — registries, associations, suggestions (Slice D, ROADMAP-0012.4) ([c2276a0](https://github.com/rutabageldev/ruby-core/commit/c2276a0172949355e499a3c8044103ac9ccde130))
* **overlay:** household overlay — tables, write handlers, read endpoints (Slice D core) ([0c3dc1d](https://github.com/rutabageldev/ruby-core/commit/0c3dc1d3ebe8d093a2167a927bd348ba7346718b))

## [0.25.1](https://github.com/rutabageldev/ruby-core/compare/v0.25.0...v0.25.1) (2026-06-28)


### Bug Fixes

* **deploy:** build api image in release and gate it behind a compose profile ([94abe89](https://github.com/rutabageldev/ruby-core/commit/94abe890dceca4d3e62d03eae4c9db44dd1befb7))
* **deploy:** build api image in release and gate it behind a compose profile ([960b781](https://github.com/rutabageldev/ruby-core/commit/960b78177b31f5ab3d3f05c0ff371bc4690d2f04))

## [0.25.0](https://github.com/rutabageldev/ruby-core/compare/v0.24.2...v0.25.0) (2026-06-28)


### Features

* **api:** scaffold spec-first HTTP read API platform (Slice A) ([7600081](https://github.com/rutabageldev/ruby-core/commit/76000813bcdc41a4c9033248c696a629cc9a2d1b))
* **api:** spec-first HTTP read API platform (Slice A, ROADMAP-0012.1) ([4ff1b26](https://github.com/rutabageldev/ruby-core/commit/4ff1b2636a07cf4fb7937473eda1fc7b1252dedd))
* **calendar:** calendar core — mirror, bidirectional sync, reminders, read endpoint (Slice C, ROADMAP-0012.3) ([8eccac1](https://github.com/rutabageldev/ruby-core/commit/8eccac1de4614ec5f544ae2d966030b774e05200))
* **calendar:** reminders — NATS due signal + HA status sensor (Slice C) ([0455fcd](https://github.com/rutabageldev/ruby-core/commit/0455fcd0305bef5cf63d314c4f9556f305182e03))
* **calendar:** store, tz-aware expansion, Google client, auth helper (Slice C foundation) ([04f0644](https://github.com/rutabageldev/ruby-core/commit/04f0644a72bd32757ba7e62f05e9701ebd296d1f))
* **calendar:** write-through processor, sync poller, read endpoint (Slice C core) ([3955a41](https://github.com/rutabageldev/ruby-core/commit/3955a41f09ddb2f1e878d5391739d8d334267a59))
* **gateway:** domain-neutral ruby_home_event write path (Slice B, ROADMAP-0012.2) ([d3a81f2](https://github.com/rutabageldev/ruby-core/commit/d3a81f2bdafaec28968b04d9d911f7a54c42e302))
* **gateway:** dual-subscribe ruby_home_event write path (Slice B) ([76652e5](https://github.com/rutabageldev/ruby-core/commit/76652e56bf56b035769fa8999e157fa91f53d332))


### Bug Fixes

* **build:** restore go.mod directive to 1.25.0 (CI toolchain match) ([e3bb3dd](https://github.com/rutabageldev/ruby-core/commit/e3bb3ddec61dad931712e662483290c8ebe6a6fc))

## [0.24.2](https://github.com/rutabageldev/ruby-core/compare/v0.24.1...v0.24.2) (2026-06-27)


### Bug Fixes

* **nats:** auto-recover NATS after host reboot (ADR-0039) ([d3a4e15](https://github.com/rutabageldev/ruby-core/commit/d3a4e151240ae878c284219e0619e7211bc6657b))
* **nats:** auto-recover NATS after host reboot (ADR-0039) ([d1bac53](https://github.com/rutabageldev/ruby-core/commit/d1bac534a75a12aed0f2fd7e897d9ed9167e3637))

## [0.24.1](https://github.com/rutabageldev/ruby-core/compare/v0.24.0...v0.24.1) (2026-06-21)


### Bug Fixes

* **ada:** recompute medication next_due on every dose event ([a5cce04](https://github.com/rutabageldev/ruby-core/commit/a5cce04281ab822b9d73e79c109714bd274bc926))
* **ada:** recompute medication next_due on every dose event ([4d1e15a](https://github.com/rutabageldev/ruby-core/commit/4d1e15ab734b30346147d144950f78aecf57a62c))

## [0.24.0](https://github.com/rutabageldev/ruby-core/compare/v0.23.0...v0.24.0) (2026-06-21)


### Features

* **ada:** emergency card + seed + docs — completes ROADMAP-0011 (0011.4) ([1ad9056](https://github.com/rutabageldev/ruby-core/commit/1ad9056947c979ba4c1b1c6f5bac8be7162469ee))
* **ada:** emergency card + seed coverage + docs (ROADMAP-0011.4) ([bc57ed5](https://github.com/rutabageldev/ruby-core/commit/bc57ed5afbeacc353e58e5cf85de326983804ba6))

## [0.23.0](https://github.com/rutabageldev/ruby-core/compare/v0.22.0...v0.23.0) (2026-06-21)


### Features

* **ada:** server-owned medication safety computations (ROADMAP-0011.3) ([62f4575](https://github.com/rutabageldev/ruby-core/commit/62f4575ebd3065d5823d9c8eebc53d0838001636))
* **ada:** server-owned medication safety computations (ROADMAP-0011.3) ([4808785](https://github.com/rutabageldev/ruby-core/commit/4808785491499c409aee22b8ffd3a83222d33b33))


### Bug Fixes

* **ada:** drop medication_id FKs — events process concurrently out of order ([1e3f8a0](https://github.com/rutabageldev/ruby-core/commit/1e3f8a0e8e2a55edb69c79f5e47b6f3e4efe1b80))

## [0.22.0](https://github.com/rutabageldev/ruby-core/compare/v0.21.0...v0.22.0) (2026-06-21)


### Features

* **ada:** medication dose events + history/edit-delete (ROADMAP-0011.2) ([6469e13](https://github.com/rutabageldev/ruby-core/commit/6469e13cc34111a6a1fa256942b14d693218e516))
* **ada:** medication dose events + series + history/edit-delete (0011.2) ([e1d2df2](https://github.com/rutabageldev/ruby-core/commit/e1d2df238b18abfd5da06c128546b7a971bb7cb3))


### Bug Fixes

* **ada:** 000008 deployed as UUID — convert medication ids to TEXT via 000009 ([088acbc](https://github.com/rutabageldev/ruby-core/commit/088acbc1d2c5ae56044235dfa1340ebaded3172b))
* **ada:** medication ids are dashboard-provided strings, not UUIDs ([21af52f](https://github.com/rutabageldev/ruby-core/commit/21af52f3c318b8ea438129f9c12815744da32703))

## [0.21.0](https://github.com/rutabageldev/ruby-core/compare/v0.20.0...v0.21.0) (2026-06-21)


### Features

* **ada:** medications schema + registry/routines CRUD (ROADMAP-0011.1) ([63bd1ad](https://github.com/rutabageldev/ruby-core/commit/63bd1ad1cf822863fd2f6f2743cfd30775f1ff72))
* **ada:** medications schema + registry/routines CRUD (ROADMAP-0011.1) ([a6c82d5](https://github.com/rutabageldev/ruby-core/commit/a6c82d571f0c214c08772af1c43cda2055df9c52))

## [0.20.0](https://github.com/rutabageldev/ruby-core/compare/v0.19.0...v0.20.0) (2026-06-19)


### Features

* **ada:** birth-watcher — snapshot (pg_dump) then nuke on ada.born (ADR-0036) ([fa8bf07](https://github.com/rutabageldev/ruby-core/commit/fa8bf0745df7b10c43c54f47bfc9ca067b07f57e))
* **ada:** birth-watcher — snapshot then nuke on ada.born (ADR-0036) ([834984c](https://github.com/rutabageldev/ruby-core/commit/834984c1c2e8c35e8b288aa5cf399af288456602))

## [0.19.0](https://github.com/rutabageldev/ruby-core/compare/v0.18.0...v0.19.0) (2026-06-19)


### Features

* **ada:** clean slate at birth — force test pre-birth + wipe tracking on ada.born (ADR-0035) ([1465a79](https://github.com/rutabageldev/ruby-core/commit/1465a791d779a075a4a85f1be2e383fd4bd94c1d))
* **ada:** clean slate at birth — force test pre-birth + wipe tracking on ada.born (ADR-0035) ([360d149](https://github.com/rutabageldev/ruby-core/commit/360d14922fd04da8b2d83ebcc261052e9f5fbb60))

## [0.18.0](https://github.com/rutabageldev/ruby-core/compare/v0.17.1...v0.18.0) (2026-06-19)


### Features

* **ada:** trends aggregation via ada.trends.query -&gt; sensor.ada_trends ([#82](https://github.com/rutabageldev/ruby-core/issues/82), ADR-0032) ([0b01b48](https://github.com/rutabageldev/ruby-core/commit/0b01b48e83a8922f7893441d45e016ca344baf29))
* **ada:** Trends aggregation via ada.trends.query -&gt; sensor.ada_trends (ROADMAP-0010.7, [#82](https://github.com/rutabageldev/ruby-core/issues/82)) ([8c364d1](https://github.com/rutabageldev/ruby-core/commit/8c364d1c562793e49c4cf3e097a98146e2e3d0df))

## [0.17.1](https://github.com/rutabageldev/ruby-core/compare/v0.17.0...v0.17.1) (2026-06-19)


### Bug Fixes

* **natsx:** bound JetStream stream retention + reconcile existing streams (ADR-0034) ([c1a175a](https://github.com/rutabageldev/ruby-core/commit/c1a175a7920666ef35ff9101e084c57d57d96158))
* **natsx:** bound JetStream stream retention and reconcile existing streams (ADR-0034) ([8f03577](https://github.com/rutabageldev/ruby-core/commit/8f035777edf4a1725302d19d0a88c92408dbbdd9))

## [0.17.0](https://github.com/rutabageldev/ruby-core/compare/v0.16.1...v0.17.0) (2026-06-18)


### Features

* **ada:** add test-data marker column and persist it through ingestion (ADR-0031) ([dc65f2b](https://github.com/rutabageldev/ruby-core/commit/dc65f2b6432b1984b70105dfb19d0573ed139c34))
* **ada:** edit & delete operations for all event types ([#77](https://github.com/rutabageldev/ruby-core/issues/77), [#78](https://github.com/rutabageldev/ruby-core/issues/78), [#79](https://github.com/rutabageldev/ruby-core/issues/79)) ([d0d6f0f](https://github.com/rutabageldev/ruby-core/commit/d0d6f0f17e96be3198707a347c4004253730a180))
* **ada:** edit/delete, test-data lifecycle + engine HA-push gate (ROADMAP-0010.4-0.6) ([3dd683b](https://github.com/rutabageldev/ruby-core/commit/3dd683b71575059f9543a8a51c80b16422b75243))
* **ada:** test-data lifecycle — guarded seed + clear make targets (ROADMAP-0010.6) ([a92931d](https://github.com/rutabageldev/ruby-core/commit/a92931db6624999addaf0bf3e0c899d8caf6b9da))


### Bug Fixes

* **engine:** gate Ada HA push behind HA_INGEST_ENABLED (ADR-0033) ([3eb45d8](https://github.com/rutabageldev/ruby-core/commit/3eb45d8baeda6cb88c18eaed1662abf965586a76))

## [0.16.1](https://github.com/rutabageldev/ruby-core/compare/v0.16.0...v0.16.1) (2026-06-18)


### Bug Fixes

* **deploy:** validate NATS PKI AppRole material, not decommissioned KV cert bundle ([84d923d](https://github.com/rutabageldev/ruby-core/commit/84d923d58a5f872c92586c9329b9214e6a605b28))
* **deploy:** validate NATS PKI AppRole material, not decommissioned KV cert bundle ([7f5e854](https://github.com/rutabageldev/ruby-core/commit/7f5e854f6e556e72238a36217e97100a8a6f276b))

## [0.16.0](https://github.com/rutabageldev/ruby-core/compare/v0.15.2...v0.16.0) (2026-06-18)


### Features

* **ada:** data-integrity fixes + feed-claim lifecycle (ROADMAP-0010.2-0010.3) ([280134b](https://github.com/rutabageldev/ruby-core/commit/280134bb13dcfe470f6267e6c545e607db5fb472))
* **ada:** implement feed-claim lifecycle ([#19](https://github.com/rutabageldev/ruby-core/issues/19), [#81](https://github.com/rutabageldev/ruby-core/issues/81)) ([f329213](https://github.com/rutabageldev/ruby-core/commit/f329213d2d6741e35525127a270dfc864e8d4c84))


### Bug Fixes

* **ada:** data-integrity fixes for last_*, tummy history, supplement, growth attribution ([cb69ba2](https://github.com/rutabageldev/ruby-core/commit/cb69ba24fdf8806100774a0747cb553376a2191e))

## [0.15.2](https://github.com/rutabageldev/ruby-core/compare/v0.15.1...v0.15.2) (2026-06-17)


### Bug Fixes

* **dev:** point dev engine at its own isolated database ([28813cc](https://github.com/rutabageldev/ruby-core/commit/28813cc3d5ac9f4c8996f47005a7c5e700102d54))
* **gateway:** gate Home Assistant ingestion to prod only ([5f776bd](https://github.com/rutabageldev/ruby-core/commit/5f776bd110e1fbd6257f738c19177527de6171e2))
* isolate non-prod environments from prod HA + database (stop 3x Ada writes) ([da7d012](https://github.com/rutabageldev/ruby-core/commit/da7d012822749c6de8838f013dc81b60a6a153d2))

## [0.15.1](https://github.com/rutabageldev/ruby-core/compare/v0.15.0...v0.15.1) (2026-05-21)


### Bug Fixes

* **deploy-prod:** force-recreate nats-cert-renewer to pick up script changes ([95ef550](https://github.com/rutabageldev/ruby-core/commit/95ef5509b5abf62d9a9b372defda15ea6a13152f))
* **deploy-prod:** force-recreate nats-cert-renewer to pick up script changes (closes [#66](https://github.com/rutabageldev/ruby-core/issues/66)) ([465a2b3](https://github.com/rutabageldev/ruby-core/commit/465a2b3cb1737319d32d531d78a800514f1020e7))

## [0.15.0](https://github.com/rutabageldev/ruby-core/compare/v0.14.0...v0.15.0) (2026-05-21)


### Features

* admin PKI migration + mkcert decommission (PLAN-0008 Stage 4.B) ([74754f8](https://github.com/rutabageldev/ruby-core/commit/74754f872f76e1b06f2dad440c3311d792fd4ee4))
* admin PKI migration + mkcert decommission (PLAN-0008 Stage 4.B) ([f24c5fb](https://github.com/rutabageldev/ruby-core/commit/f24c5fb8d237e007c8cb03ded41163957def6e7d))

## [0.14.0](https://github.com/rutabageldev/ruby-core/compare/v0.13.0...v0.14.0) (2026-05-21)


### Features

* **deploy/prod:** direct-PKI + nats-cert-renewer (PLAN-0008 Stage 4.A) ([3d05d35](https://github.com/rutabageldev/ruby-core/commit/3d05d3539ba37f1167544f41c21e9c2dc1885903))
* **deploy/prod:** direct-PKI + nats-cert-renewer (PLAN-0008 Stage 4.A) ([f483d14](https://github.com/rutabageldev/ruby-core/commit/f483d1445b6d59f7bf4e5ef9d561bf673daa794b))

## [0.13.0](https://github.com/rutabageldev/ruby-core/compare/v0.12.0...v0.13.0) (2026-05-21)


### Features

* **boot:** direct-PKI cert issuance via AppRole + in-process renewal (Phase 17.6.2) ([11d760b](https://github.com/rutabageldev/ruby-core/commit/11d760b94bee9ae6c123061251ada56fea01af59))
* **boot:** direct-PKI cert issuance via AppRole + in-process renewal (Phase 17.6.2) ([c4f9af9](https://github.com/rutabageldev/ruby-core/commit/c4f9af989d9b2ea90167a1f7a64730092b0fd06f))
* **deploy/dev:** nats-cert-renewer sidecar for automatic NATS server cert rotation ([ea75b27](https://github.com/rutabageldev/ruby-core/commit/ea75b27eee5ec52bedc34090ea7aab792693a4dd))
* **deploy/dev:** nats-cert-renewer sidecar for automatic NATS server cert rotation ([b29541c](https://github.com/rutabageldev/ruby-core/commit/b29541c2ee98200e131c44fd03cb46b188a46cea))
* **deploy/staging:** direct-PKI + nats-cert-renewer (PLAN-0008 Stage 3) ([ed2a722](https://github.com/rutabageldev/ruby-core/commit/ed2a722e964a4a9efebdbcf2490650fb20a90114))
* **deploy/staging:** direct-PKI + nats-cert-renewer (PLAN-0008 Stage 3) ([415dc19](https://github.com/rutabageldev/ruby-core/commit/415dc1937d7b8f29fc57e552c216a0cd14269749))


### Bug Fixes

* **deploy/staging:** pull_policy=never on nats-cert-renewer (local-only image) ([c488686](https://github.com/rutabageldev/ruby-core/commit/c48868617af3e71a41c60fe67b327adb12cdd135))
* **pki:** bundle mkcert CA into NATS ca.pem + smoke-test trust bundle ([7ad23d2](https://github.com/rutabageldev/ruby-core/commit/7ad23d2b7eaf83da6b70e234afcde3a0d31570db))
* **release:** always emit raw v-prefix tag (unblocks prerelease deploys) ([b8ebb7e](https://github.com/rutabageldev/ruby-core/commit/b8ebb7e9799fc30f0e8b8cd34aa6d708bcc85160))

## [0.12.0](https://github.com/rutabageldev/ruby-core/compare/v0.11.4...v0.12.0) (2026-04-30)


### Features

* add release-please for automated release PR management (PLAN-0017 step 10) ([58ac5cc](https://github.com/rutabageldev/ruby-core/commit/58ac5cceb2b25d328f01d6445489aa7b0c06d945))
* release-please automated release PR management (PLAN-0017 step 10) ([06e0cb0](https://github.com/rutabageldev/ruby-core/commit/06e0cb01ff75d9c00f0be8fc406b7081051d2a6c))


### Bug Fixes

* add bootstrap-sha to release-please config ([#49](https://github.com/rutabageldev/ruby-core/issues/49)) ([75411af](https://github.com/rutabageldev/ruby-core/commit/75411af17822c343aa54eee1f0ad2d3f9d7d0cd1))
* add packages key to release-please-config.json ([02ad912](https://github.com/rutabageldev/ruby-core/commit/02ad912b44fba3b944fadb817771df4efde904db))
* add packages key to release-please-config.json ([4e0dee9](https://github.com/rutabageldev/ruby-core/commit/4e0dee92020e3c634f6be08ce55f873e23064e31))
* add workflow_dispatch to release-please for manual triggering ([#48](https://github.com/rutabageldev/ruby-core/issues/48)) ([f9f4136](https://github.com/rutabageldev/ruby-core/commit/f9f41365a719fa674ca4b016bf59ae1d18bf1a2b))
* archive completed plans and remove stale duplicates ([#47](https://github.com/rutabageldev/ruby-core/issues/47)) ([7ccd62b](https://github.com/rutabageldev/ruby-core/commit/7ccd62bb6b792d643f7766f2b7c6fca80fff0fe2))
* ensure log directory exists before writing stability log ([dc019e7](https://github.com/rutabageldev/ruby-core/commit/dc019e78fe1b643a5d82e001d96cc8ecc8b61cc0))
* ensure log directory exists before writing stability log ([35e6192](https://github.com/rutabageldev/ruby-core/commit/35e619211f42ac70d2147e662660b699e26bc6ed))
