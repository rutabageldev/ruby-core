-- ============================ directory_person ============================

-- name: ListActivePeople :many
SELECT * FROM directory_person WHERE active ORDER BY display_name;

-- name: ListPeopleByIDs :many
SELECT * FROM directory_person WHERE id = ANY(@ids::uuid[]);

-- name: GetPersonByEmail :one
SELECT * FROM directory_person WHERE lower(email) = lower(@email) LIMIT 1;

-- DeactivatePerson soft-deletes a person (#155 §3) — the row is retained so historical
-- event associations still resolve.
-- name: DeactivatePerson :exec
UPDATE directory_person SET active = false WHERE id = @id;

-- name: UpsertPerson :exec
INSERT INTO directory_person (id, display_name, kind, ha_person_entity_id, email, family, color, active)
VALUES (@id, @display_name, @kind, @ha_person_entity_id, @email, @family, @color, @active)
ON CONFLICT (id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    kind = EXCLUDED.kind,
    ha_person_entity_id = EXCLUDED.ha_person_entity_id,
    email = EXCLUDED.email,
    family = EXCLUDED.family,
    color = EXCLUDED.color,
    active = EXCLUDED.active;

-- ============================ person_email ============================

-- person_email holds a directory person's alias / secondary addresses (#133); the
-- primary stays on directory_person.email. Attendee reconciliation matches either source.

-- name: UpsertPersonEmail :exec
INSERT INTO person_email (person_id, email)
VALUES (@person_id, @email)
ON CONFLICT (lower(email)) DO UPDATE SET person_id = EXCLUDED.person_id;

-- ListAllPersonEmails returns every alias email -> person mapping for the read index.
-- name: ListAllPersonEmails :many
SELECT person_id, email FROM person_email;

-- ============================ childcare_provider ============================

-- name: ListActiveProviders :many
SELECT * FROM childcare_provider WHERE NOT archived ORDER BY display_name;

-- name: ListProvidersByIDs :many
SELECT * FROM childcare_provider WHERE id = ANY(@ids::uuid[]);

-- name: UpsertProvider :exec
INSERT INTO childcare_provider (id, display_name, person_id, relationship, archived)
VALUES (@id, @display_name, @person_id, @relationship, @archived)
ON CONFLICT (id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    person_id = EXCLUDED.person_id,
    relationship = EXCLUDED.relationship,
    archived = EXCLUDED.archived;

-- ArchiveProvider soft-deletes (delete = archive, preserving frequency history).
-- name: ArchiveProvider :exec
UPDATE childcare_provider SET archived = true WHERE id = @id;

-- ============================ associations ============================

-- name: DeleteEventSubjects :exec
DELETE FROM event_subject WHERE google_event_id = @google_event_id;

-- name: InsertEventSubject :exec
INSERT INTO event_subject (google_event_id, person_id)
VALUES (@google_event_id, @person_id)
ON CONFLICT (google_event_id, person_id) DO NOTHING;

-- name: DeleteEventChildcare :exec
DELETE FROM event_childcare WHERE google_event_id = @google_event_id;

-- name: InsertEventChildcare :exec
INSERT INTO event_childcare (google_event_id, provider_id)
VALUES (@google_event_id, @provider_id)
ON CONFLICT (google_event_id, provider_id) DO NOTHING;

-- ListSubjectsForEvents returns subject associations for a set of events.
-- name: ListSubjectsForEvents :many
SELECT google_event_id, person_id FROM event_subject
WHERE google_event_id = ANY(@event_ids::text[]);

-- ListChildcareForEvents returns childcare associations for a set of events.
-- name: ListChildcareForEvents :many
SELECT google_event_id, provider_id FROM event_childcare
WHERE google_event_id = ANY(@event_ids::text[]);

-- ListProviderEvents returns, for every non-archived provider, the calendar_event
-- rows it is associated with — the input to per-occurrence suggestion ranking.
-- name: ListProviderEvents :many
SELECT ec.provider_id, ce.*
FROM event_childcare ec
JOIN calendar_event ce ON ce.google_event_id = ec.google_event_id
JOIN childcare_provider p ON p.id = ec.provider_id
WHERE NOT p.archived AND ce.status <> 'cancelled';
