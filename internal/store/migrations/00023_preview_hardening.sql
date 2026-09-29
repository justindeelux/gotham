-- +goose Up
-- Preview hardening (Phase 8, BE-8.1 fix round 1).
--
-- Two pieces:
--
-- 1. applications.is_preview marks a sibling application created by the
--    preview controller. The marker makes a preview recognizable without a
--    naming heuristic, which is what the teardown paths need:
--      - deleting a preview (user or system) must NOT remove the shared
--        remote deploy key (the base application keeps using it), and
--      - the orphan sweep must be able to find a preview application whose
--        binding is gone (crash between clone and binding write, or a lost
--        base delete) without matching ordinary user applications.
--
-- 2. preview_deliveries is the preview-specific delivery reservation ledger.
--    BE-8.1 originally claimed pull_request deliveries in webhook_events
--    (application_id, commit_sha), which is the PUSH ledger: a PR start at a
--    commit suppressed a later push of that commit on the base application, a
--    second PR at the same head was treated as a duplicate, and a reopen at
--    the close head was suppressed because the old claim was never released.
--    PR idempotency now lives here instead:
--      - a start reserves (base application, pr_number, head_sha) — the
--        signed body's identity, so a replayed body is a duplicate while a
--        new synchronize (new SHA) is a new reservation;
--      - a close reserves (base application, pr_number) — concurrent or
--        redelivered closes are a no-op;
--      - closing clears the PR's reservations, so a reopen (even at the same
--        head) can reserve again;
--      - push keeps using webhook_events unchanged (migration 00007).
--    delivery_id is stored for the audit trail only: it is an unsigned header
--    and never gates a delivery on its own.
CREATE TABLE preview_deliveries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    pr_number int NOT NULL,
    kind text NOT NULL,
    head_sha text NOT NULL DEFAULT '',
    delivery_id text NOT NULL DEFAULT '',
    received_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT preview_deliveries_kind_check CHECK (kind IN ('start', 'close')),
    CONSTRAINT preview_deliveries_pr_number_check CHECK (pr_number > 0)
);

-- Signed-body replay protection: one start reservation per (base app, PR,
-- head). A different PR or a new head is independent; the same head after a
-- close is allowed because the close clears this PR's reservations.
CREATE UNIQUE INDEX preview_deliveries_start_idx
    ON preview_deliveries (application_id, pr_number, head_sha)
    WHERE kind = 'start' AND head_sha <> '';

-- One close reservation per PR: a concurrent or redelivered close is a no-op.
CREATE UNIQUE INDEX preview_deliveries_close_idx
    ON preview_deliveries (application_id, pr_number)
    WHERE kind = 'close';

-- Clears a PR's reservations on close and lists them for the audit trail.
CREATE INDEX preview_deliveries_app_pr_idx
    ON preview_deliveries (application_id, pr_number);

ALTER TABLE applications ADD COLUMN is_preview boolean NOT NULL DEFAULT false;

-- The orphan sweep's work list: preview applications ordered by age.
CREATE INDEX applications_preview_idx
    ON applications (created_at)
    WHERE is_preview;

-- +goose Down
DROP INDEX IF EXISTS applications_preview_idx;
ALTER TABLE applications DROP COLUMN IF EXISTS is_preview;

DROP TABLE IF EXISTS preview_deliveries;
