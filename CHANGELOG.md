# Changelog

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
