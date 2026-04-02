-- Ada database seed
-- All rows use logged_by = 'seed' for targeted cleanup via db-seed-clear.
-- FK cascades (feeding_bottle_detail, feeding_segments) are handled automatically
-- when parent feedings are deleted.
--
-- Coverage:
--   feedings:  breast_left, breast_right, breast (both sides), bottle_breast,
--              bottle_formula, mixed, breast+supplement (breast + bottle_detail)
--   diapers:   wet, dirty, mixed
--   sleep:     night (completed), nap x2 (completed)
--   tummy:     2 sessions
--   ada_config: feed_interval_hours, next_feeding_target

DO $seed$
DECLARE
    -- Feeding IDs — generated at seed time so segments/bottle_detail can reference them
    f_breast_left       UUID := gen_random_uuid();
    f_breast_right      UUID := gen_random_uuid();
    f_breast_both       UUID := gen_random_uuid();
    f_breast_supp       UUID := gen_random_uuid(); -- breast + supplement
    f_bottle_breast     UUID := gen_random_uuid();
    f_bottle_formula_1  UUID := gen_random_uuid();
    f_mixed             UUID := gen_random_uuid();
    f_bottle_formula_2  UUID := gen_random_uuid(); -- recent, inside 24h
    f_breast_both_2     UUID := gen_random_uuid(); -- most recent
BEGIN

    -- ── Feedings ──────────────────────────────────────────────────────────────

    -- breast_left only (outside 24h window)
    INSERT INTO feedings (id, timestamp, source, logged_by)
    VALUES (f_breast_left, NOW() - INTERVAL '34 hours', 'breast_left', 'seed');

    INSERT INTO feeding_segments (feeding_id, side, started_at, ended_at, duration_s)
    VALUES (f_breast_left,
            'left',
            NOW() - INTERVAL '34 hours',
            NOW() - INTERVAL '34 hours' + INTERVAL '12 minutes',
            720);

    -- breast_right only (outside 24h window)
    INSERT INTO feedings (id, timestamp, source, logged_by)
    VALUES (f_breast_right, NOW() - INTERVAL '28 hours', 'breast_right', 'seed');

    INSERT INTO feeding_segments (feeding_id, side, started_at, ended_at, duration_s)
    VALUES (f_breast_right,
            'right',
            NOW() - INTERVAL '28 hours',
            NOW() - INTERVAL '28 hours' + INTERVAL '10 minutes',
            600);

    -- breast both sides — 2 segments (outside 24h window)
    INSERT INTO feedings (id, timestamp, source, logged_by)
    VALUES (f_breast_both, NOW() - INTERVAL '22 hours', 'breast', 'seed');

    INSERT INTO feeding_segments (feeding_id, side, started_at, ended_at, duration_s)
    VALUES
        (f_breast_both, 'left',
         NOW() - INTERVAL '22 hours',
         NOW() - INTERVAL '22 hours' + INTERVAL '8 minutes', 480),
        (f_breast_both, 'right',
         NOW() - INTERVAL '22 hours' + INTERVAL '8 minutes',
         NOW() - INTERVAL '22 hours' + INTERVAL '16 minutes', 480);

    -- breast + supplement (inside 24h window)
    -- Simulates: breast session ended, then top-off bottle attached to same feeding
    INSERT INTO feedings (id, timestamp, source, logged_by)
    VALUES (f_breast_supp, NOW() - INTERVAL '18 hours', 'breast_left', 'seed');

    INSERT INTO feeding_segments (feeding_id, side, started_at, ended_at, duration_s)
    VALUES (f_breast_supp,
            'left',
            NOW() - INTERVAL '18 hours',
            NOW() - INTERVAL '18 hours' + INTERVAL '9 minutes',
            540);

    INSERT INTO feeding_bottle_detail (feeding_id, amount_oz)
    VALUES (f_breast_supp, 1.50);

    -- bottle_breast (inside 24h window)
    INSERT INTO feedings (id, timestamp, source, logged_by)
    VALUES (f_bottle_breast, NOW() - INTERVAL '14 hours', 'bottle_breast', 'seed');

    INSERT INTO feeding_bottle_detail (feeding_id, amount_oz)
    VALUES (f_bottle_breast, 3.00);

    -- bottle_formula first (inside 24h window)
    INSERT INTO feedings (id, timestamp, source, logged_by)
    VALUES (f_bottle_formula_1, NOW() - INTERVAL '10 hours', 'bottle_formula', 'seed');

    INSERT INTO feeding_bottle_detail (feeding_id, amount_oz)
    VALUES (f_bottle_formula_1, 4.00);

    -- mixed bottle (inside 24h window)
    INSERT INTO feedings (id, timestamp, source, logged_by)
    VALUES (f_mixed, NOW() - INTERVAL '6 hours', 'mixed', 'seed');

    INSERT INTO feeding_bottle_detail (feeding_id, amount_oz, breast_milk_oz, formula_oz)
    VALUES (f_mixed, 3.50, 2.00, 1.50);

    -- bottle_formula second, recent (inside 24h window)
    INSERT INTO feedings (id, timestamp, source, logged_by)
    VALUES (f_bottle_formula_2, NOW() - INTERVAL '3 hours', 'bottle_formula', 'seed');

    INSERT INTO feeding_bottle_detail (feeding_id, amount_oz)
    VALUES (f_bottle_formula_2, 3.50);

    -- breast both sides, most recent (inside 24h window)
    INSERT INTO feedings (id, timestamp, source, logged_by)
    VALUES (f_breast_both_2, NOW() - INTERVAL '1 hour', 'breast', 'seed');

    INSERT INTO feeding_segments (feeding_id, side, started_at, ended_at, duration_s)
    VALUES
        (f_breast_both_2, 'right',
         NOW() - INTERVAL '1 hour',
         NOW() - INTERVAL '1 hour' + INTERVAL '10 minutes', 600),
        (f_breast_both_2, 'left',
         NOW() - INTERVAL '1 hour' + INTERVAL '10 minutes',
         NOW() - INTERVAL '1 hour' + INTERVAL '17 minutes', 420);

    -- ── Diapers ───────────────────────────────────────────────────────────────

    INSERT INTO diapers (timestamp, type, logged_by) VALUES
        (NOW() - INTERVAL '30 hours', 'wet',   'seed'),
        (NOW() - INTERVAL '24 hours', 'dirty', 'seed'),
        (NOW() - INTERVAL '19 hours', 'wet',   'seed'),
        (NOW() - INTERVAL '15 hours', 'mixed', 'seed'),
        (NOW() - INTERVAL '11 hours', 'dirty', 'seed'),
        (NOW() - INTERVAL '7 hours',  'wet',   'seed'),
        (NOW() - INTERVAL '4 hours',  'mixed', 'seed'),
        (NOW() - INTERVAL '90 minutes', 'wet', 'seed');

    -- ── Sleep sessions ────────────────────────────────────────────────────────

    -- Night sleep (completed, spans outside→inside 24h window)
    INSERT INTO sleep_sessions (start_time, end_time, sleep_type, logged_by)
    VALUES (NOW() - INTERVAL '26 hours', NOW() - INTERVAL '18 hours', 'night', 'seed');

    -- Nap 1 (completed, inside 24h window)
    INSERT INTO sleep_sessions (start_time, end_time, sleep_type, logged_by)
    VALUES (NOW() - INTERVAL '13 hours', NOW() - INTERVAL '11 hours 30 minutes', 'nap', 'seed');

    -- Nap 2 (completed, recent)
    INSERT INTO sleep_sessions (start_time, end_time, sleep_type, logged_by)
    VALUES (NOW() - INTERVAL '5 hours', NOW() - INTERVAL '4 hours', 'nap', 'seed');

    -- ── Tummy time ────────────────────────────────────────────────────────────

    INSERT INTO tummy_time_sessions (start_time, end_time, duration_s, logged_by)
    VALUES
        (NOW() - INTERVAL '12 hours',
         NOW() - INTERVAL '12 hours' + INTERVAL '10 minutes',
         600, 'seed'),
        (NOW() - INTERVAL '3 hours 30 minutes',
         NOW() - INTERVAL '3 hours 30 minutes' + INTERVAL '15 minutes',
         900, 'seed');

    -- ── Ada config ────────────────────────────────────────────────────────────
    -- Upsert so seed doesn't conflict with existing production config values.
    -- The clear script will only remove these if no non-seed config exists.

    INSERT INTO ada_config (key, value, updated_at)
    VALUES
        ('feed_interval_hours', '3.0', NOW()),
        ('next_feeding_target',
         TO_CHAR(NOW() - INTERVAL '1 hour' + INTERVAL '3 hours', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
         NOW())
    ON CONFLICT (key) DO UPDATE
        SET value      = EXCLUDED.value,
            updated_at = EXCLUDED.updated_at;

END $seed$;
