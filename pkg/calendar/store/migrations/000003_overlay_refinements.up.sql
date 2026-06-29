-- Overlay refinements (ROADMAP-0012, PLAN-0035):
--   #134: constrain childcare_provider.relationship to a starter vocabulary (NULL still
--         allowed; text + CHECK per the overlay convention — extend later with another ALTER).
--   #133: person_email side table so attendee reconciliation can match a person's alias or
--         secondary addresses, not just the primary directory_person.email.

-- #134 — normalize any out-of-vocabulary values first so ADD CONSTRAINT cannot fail on
-- pre-existing free-text data, then constrain.
UPDATE childcare_provider
SET relationship = 'other'
WHERE relationship IS NOT NULL
  AND relationship NOT IN (
    'grandparent', 'sibling', 'aunt_uncle', 'nanny',
    'daycare', 'babysitter', 'friend', 'neighbour', 'other'
  );

ALTER TABLE childcare_provider
    ADD CONSTRAINT childcare_provider_relationship_check
    CHECK (relationship IS NULL OR relationship IN (
        'grandparent', 'sibling', 'aunt_uncle', 'nanny',
        'daycare', 'babysitter', 'friend', 'neighbour', 'other'
    ));

-- #133 — additional emails per directory person (aliases / secondary addresses). The
-- primary email stays on directory_person.email; reconciliation matches either source.
CREATE TABLE person_email (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id   uuid NOT NULL REFERENCES directory_person (id) ON DELETE CASCADE,
    email       text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Case-insensitive uniqueness across alias emails.
CREATE UNIQUE INDEX person_email_lower_idx ON person_email (lower(email));

CREATE INDEX person_email_person_idx ON person_email (person_id);
