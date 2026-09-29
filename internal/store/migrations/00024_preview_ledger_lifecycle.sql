-- +goose Up
-- Preview ledger lifecycle (Phase 8, BE-8.1 fix round 2).
--
-- 1. preview_deploys gains the 'closing' state. A close delivery persists the
--    close intent before tearing the sibling down, so a teardown that fails
--    (or a delivery that is never retried) is picked up by the sweep instead
--    of leaving the preview running forever: the sweep re-attempts teardown
--    for bindings left in 'closing'.
ALTER TABLE preview_deploys DROP CONSTRAINT preview_deploys_state_check;
ALTER TABLE preview_deploys ADD CONSTRAINT preview_deploys_state_check CHECK (
    state IN ('active', 'deploying', 'failed', 'closing', 'deleted')
);

-- The sweep's closing work list.
CREATE INDEX preview_deploys_closing_idx
    ON preview_deploys (updated_at)
    WHERE state = 'closing';

-- 2. preview_deliveries rows are now in-flight leases, not a permanent
--    handled-SHA set: a start lease lives only while its delivery is being
--    processed (the live binding's head is the post-success dedupe source),
--    and a lease that outlives its delivery — a crash — expires so it can
--    never suppress a later delivery of the same revision. Close reservations
--    expire too (teardown is idempotent, so a racing retry is safe).
--
--    The column default gives every new reservation a bounded lifetime; the
--    claim purges expired rows of the PR it is processing, and the sweep
--    purges globally. Rows written under the old permanent semantics are
--    discarded: they are coordination state, not audit (the bindings in
--    preview_deploys are the audit trail).
ALTER TABLE preview_deliveries
    ADD COLUMN expires_at timestamptz NOT NULL DEFAULT now() + interval '15 minutes';

DELETE FROM preview_deliveries;

CREATE INDEX preview_deliveries_expiry_idx
    ON preview_deliveries (expires_at);

-- +goose Down
DROP INDEX IF EXISTS preview_deliveries_expiry_idx;
ALTER TABLE preview_deliveries DROP COLUMN IF EXISTS expires_at;
DELETE FROM preview_deliveries;

DROP INDEX IF EXISTS preview_deploys_closing_idx;
ALTER TABLE preview_deploys DROP CONSTRAINT preview_deploys_state_check;
ALTER TABLE preview_deploys ADD CONSTRAINT preview_deploys_state_check CHECK (
    state IN ('active', 'deploying', 'failed', 'deleted')
);
