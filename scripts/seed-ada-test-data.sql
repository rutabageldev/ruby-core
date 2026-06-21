-- seed-ada-test-data.sql — representative, fully test-flagged Ada dataset (ADR-0031).
--
-- Every row is test=true and logged_by='seed', so it is selectable for teardown by
-- both this script's clear-then-seed step and the general clear target. Activity
-- spans ~14 months so rolling histories, today totals, future trend buckets, and the
-- growth chart all populate. Idempotent: re-running replaces the prior seed.
--
-- Required variable: -v dob='<RFC3339>'  (the test date of birth; growth ages derive
-- from it). Activity is anchored to now() so the recent windows are always populated.

\set ON_ERROR_STOP on
BEGIN;

-- ── 1. Clear any prior seed (logged_by='seed'); FK cascade clears feeding children ──
DELETE FROM feedings            WHERE logged_by = 'seed';
DELETE FROM diapers             WHERE logged_by = 'seed';
DELETE FROM sleep_sessions      WHERE logged_by = 'seed';
DELETE FROM tummy_time_sessions WHERE logged_by = 'seed';
DELETE FROM growth_measurements WHERE logged_by = 'seed';
DELETE FROM medication_events      WHERE logged_by = 'seed';
DELETE FROM medication_temp_series WHERE logged_by = 'seed';
DELETE FROM medication_routines    WHERE logged_by = 'seed';
DELETE FROM medications            WHERE logged_by = 'seed';
DELETE FROM emergency_rows         WHERE logged_by = 'seed';

-- ── 2. Feeds — every 3h for 420 days, cycling through all source types ──────────────
INSERT INTO feedings (timestamp, source, logged_by, test)
SELECT ts,
       (ARRAY['breast_left','breast_right','bottle_formula','bottle_breast','mixed'])
         [1 + (row_number() OVER (ORDER BY ts))::int % 5],
       'seed', true
FROM generate_series(now() - interval '420 days', now(), interval '3 hours') AS ts;

-- Breast timing segments for breast feeds.
INSERT INTO feeding_segments (feeding_id, side, started_at, ended_at, duration_s)
SELECT id, 'left', timestamp, timestamp + interval '600 seconds', 600
FROM feedings WHERE logged_by = 'seed' AND source = 'breast_left';
INSERT INTO feeding_segments (feeding_id, side, started_at, ended_at, duration_s)
SELECT id, 'right', timestamp, timestamp + interval '540 seconds', 540
FROM feedings WHERE logged_by = 'seed' AND source = 'breast_right';

-- Bottle amounts (oz) for bottle feeds; mixed carries both components.
INSERT INTO feeding_bottle_detail (feeding_id, amount_oz, breast_milk_oz, formula_oz)
SELECT id, 3.0, 0, 3.0 FROM feedings WHERE logged_by = 'seed' AND source = 'bottle_formula';
INSERT INTO feeding_bottle_detail (feeding_id, amount_oz, breast_milk_oz, formula_oz)
SELECT id, 3.0, 3.0, 0 FROM feedings WHERE logged_by = 'seed' AND source = 'bottle_breast';
INSERT INTO feeding_bottle_detail (feeding_id, amount_oz, breast_milk_oz, formula_oz)
SELECT id, 4.0, 2.0, 2.0 FROM feedings WHERE logged_by = 'seed' AND source = 'mixed';

-- ── 3. Diapers — every 2.5h, cycling wet / dirty / mixed ────────────────────────────
INSERT INTO diapers (timestamp, type, logged_by, test)
SELECT ts,
       (ARRAY['wet','wet','dirty','mixed'])
         [1 + (row_number() OVER (ORDER BY ts))::int % 4],
       'seed', true
FROM generate_series(now() - interval '420 days', now(), interval '150 minutes') AS ts;

-- ── 4. Sleep — one night + two naps per day (all ended before now) ──────────────────
INSERT INTO sleep_sessions (start_time, end_time, sleep_type, logged_by, test)
SELECT d + interval '20 hours', d + interval '30 hours', 'night', 'seed', true
FROM generate_series((now()::date - 420), now()::date - 1, interval '1 day') AS d;
INSERT INTO sleep_sessions (start_time, end_time, sleep_type, logged_by, test)
SELECT d + interval '10 hours', d + interval '11 hours 30 minutes', 'nap', 'seed', true
FROM generate_series((now()::date - 420), now()::date - 1, interval '1 day') AS d;
INSERT INTO sleep_sessions (start_time, end_time, sleep_type, logged_by, test)
SELECT d + interval '14 hours', d + interval '15 hours', 'nap', 'seed', true
FROM generate_series((now()::date - 420), now()::date - 1, interval '1 day') AS d;

-- ── 5. Tummy time — ~2/day, 5 minutes each ──────────────────────────────────────────
INSERT INTO tummy_time_sessions (start_time, end_time, duration_s, logged_by, test)
SELECT ts, ts + interval '300 seconds', 300, 'seed', true
FROM generate_series(now() - interval '420 days', now() - interval '1 hour', interval '13 hours') AS ts;

-- ── 6. Growth — 8-point WHO-channel series per metric, ages relative to :dob ─────────
-- Percentiles are intentionally left NULL: the dashboard computes them client-side
-- from value + age + the WHO curves (#80). Only points at or before now() are seeded.
INSERT INTO growth_measurements (measured_at, weight_oz, length_in, head_circumference_in, source, logged_by, test)
SELECT (:'dob'::timestamptz + (g.age_days || ' days')::interval),
       g.w, g.l, g.h,
       CASE WHEN g.age_days % 2 = 0 THEN 'home' ELSE 'pediatrician' END,
       'seed', true
FROM (VALUES
    (0,   116.0, 19.3, 13.6),
    (30,  154.0, 21.0, 14.6),
    (61,  185.0, 22.4, 15.3),
    (91,  207.0, 23.4, 15.8),
    (182, 256.0, 25.7, 16.7),
    (273, 288.0, 27.4, 17.2),
    (365, 313.0, 29.0, 17.6),
    (425, 330.0, 29.8, 17.8)
) AS g(age_days, w, l, h)
WHERE (:'dob'::timestamptz + (g.age_days || ' days')::interval) <= now();

-- ── 7. Medications — a scheduled vitamin (single 08:00 slot, never auto-missed) and
--       a PRN analgesic with safety limits, plus a week of recent doses ──────────────
INSERT INTO medications (id, name, route, measure_unit, min_interval_hours, max_per_24h, active, logged_by, test) VALUES
    ('seed-m-vitd', 'Vitamin D',      'drops', 'drops', NULL, NULL, true, 'seed', true),
    ('seed-m-tyl',  'Infant Tylenol', 'oral',  'mL',    4,    4,    true, 'seed', true);

INSERT INTO medication_routines (id, medication_id, dose_amount, schedule_type, fixed_times, interval_hours, end_type, end_value, status, logged_by, test) VALUES
    ('seed-rt-vitd', 'seed-m-vitd', 1, 'fixed_times', ARRAY['08:00'], NULL, 'none', NULL, 'active', 'seed', true);

-- Scheduled vitamin given at 08:00 for the last week.
INSERT INTO medication_events (id, medication_id, status, timestamp, routine_id, slot_time, dose_amount, dose_unit, source, logged_by, test)
SELECT 'seed-ev-vitd-' || to_char(d, 'YYYYMMDD'), 'seed-m-vitd', 'given',
       d + interval '8 hours', 'seed-rt-vitd', '08:00', 1, 'drops', 'scheduled', 'seed', true
FROM generate_series(now()::date - 6, now()::date, interval '1 day') AS d;

-- A recent PRN analgesic dose.
INSERT INTO medication_events (id, medication_id, status, timestamp, dose_amount, dose_unit, source, logged_by, test) VALUES
    ('seed-ev-tyl-1', 'seed-m-tyl', 'given', now() - interval '20 hours', 1.25, 'mL', 'prn', 'seed', true);

-- ── 8. Emergency card — contacts + the two live fields ──────────────────────────────
INSERT INTO emergency_rows (id, sort_order, type, label, name, phone, address, field_key, logged_by, test) VALUES
    ('seed-ec-ped',    0, 'contact',    'Pediatrician',   'Bellevue Pediatrics · Dr. Lee', '(425) 555-0143', '1100 Bellevue Way NE, Suite 200', NULL,             'seed', true),
    ('seed-ec-poison', 1, 'contact',    'Poison Control', NULL,                            '1-800-222-1222', NULL,                              NULL,             'seed', true),
    ('seed-ec-home',   2, 'contact',    'Home',           NULL,                            NULL,             '4218 Maple Ave, Bellevue WA',     NULL,             'seed', true),
    ('seed-ec-weight', 3, 'live_field', 'Current weight', NULL,                            NULL,             NULL,                              'current_weight', 'seed', true),
    ('seed-ec-age',    4, 'live_field', 'Age',            NULL,                            NULL,             NULL,                              'age',            'seed', true);

COMMIT;

-- ── Report ──────────────────────────────────────────────────────────────────────────
SELECT 'feedings'  AS table, count(*) FROM feedings            WHERE logged_by = 'seed'
UNION ALL SELECT 'diapers',  count(*) FROM diapers             WHERE logged_by = 'seed'
UNION ALL SELECT 'sleep',    count(*) FROM sleep_sessions      WHERE logged_by = 'seed'
UNION ALL SELECT 'tummy',    count(*) FROM tummy_time_sessions WHERE logged_by = 'seed'
UNION ALL SELECT 'growth',    count(*) FROM growth_measurements WHERE logged_by = 'seed'
UNION ALL SELECT 'meds',      count(*) FROM medications            WHERE logged_by = 'seed'
UNION ALL SELECT 'routines',  count(*) FROM medication_routines    WHERE logged_by = 'seed'
UNION ALL SELECT 'med_events', count(*) FROM medication_events     WHERE logged_by = 'seed'
UNION ALL SELECT 'emergency', count(*) FROM emergency_rows         WHERE logged_by = 'seed';
