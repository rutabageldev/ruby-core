# Deploy & Rollback Runbook

## Deploying a New Version

1. Push a Git tag to trigger the release pipeline:

   ```bash
   git tag v0.2.0
   git push origin v0.2.0
   ```

2. Wait for the GitHub Actions release workflow to build and push images to GHCR.

3. Update the `VERSION` in your production `.env` file:

   ```bash
   cd deploy/prod
   # Edit .env
   VERSION=v0.2.0
   ```

4. Pull and restart services:

   ```bash
   docker compose -f compose.prod.yaml pull
   docker compose -f compose.prod.yaml up -d
   ```

5. Verify services are healthy:

   ```bash
   docker compose -f compose.prod.yaml ps
   docker compose -f compose.prod.yaml logs --tail=20 gateway engine
   ```

## Rolling Back to a Prior Version

1. Update `VERSION` in your production `.env` to the last known-good tag:

   ```bash
   cd deploy/prod
   # Edit .env
   VERSION=v0.1.0
   ```

2. Pull the previous images and restart:

   ```bash
   docker compose -f compose.prod.yaml pull
   docker compose -f compose.prod.yaml up -d
   ```

3. Verify rollback succeeded:

   ```bash
   docker compose -f compose.prod.yaml ps
   docker compose -f compose.prod.yaml logs --tail=20 gateway engine
   ```

   Confirm the version in the startup log matches the rolled-back tag.

## Notes

- NATS is unaffected by service rollbacks (it runs independently).
- JetStream data persists across service restarts (ADR-0021).
- All GHCR images remain available by tag, so any prior version can be restored.
- If a rollback is needed urgently, skip step 1 and use `docker compose up -d` with the existing `.env` after editing `VERSION`.
