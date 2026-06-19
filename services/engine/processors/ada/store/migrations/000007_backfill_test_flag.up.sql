-- Backfill: flag all existing Ada rows as test data (ADR-0035).
-- Pre-birth, every Ada event is test data by definition, but rows written before
-- the test marker was reliably stamped landed test=false. This one-time backfill
-- marks them test=true so the first ada.born clears a complete clean slate.
-- Irreversible: original test=false values are not recoverable (down is a no-op).
UPDATE feedings            SET test = true WHERE test = false;
UPDATE diapers             SET test = true WHERE test = false;
UPDATE sleep_sessions      SET test = true WHERE test = false;
UPDATE tummy_time_sessions SET test = true WHERE test = false;
UPDATE growth_measurements SET test = true WHERE test = false;
