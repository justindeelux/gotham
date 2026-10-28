-- +goose Up
-- Reliable notification delivery (GOTHAM-89). Delivery work is persisted before
-- the worker is woken, so a control-plane restart or a crash between enqueue and
-- send resumes eligible work from the database instead of losing it. The
-- existing per-channel history table becomes the queue: one row per
-- event/channel with a stable local identity.
ALTER TABLE notification_deliveries
    DROP CONSTRAINT notification_deliveries_status_check;

UPDATE notification_deliveries
SET status = CASE status WHEN 'success' THEN 'delivered' ELSE 'terminal_failed' END;

ALTER TABLE notification_deliveries
    ADD CONSTRAINT notification_deliveries_status_check
    CHECK (status IN ('pending', 'retrying', 'sending', 'delivered', 'terminal_failed', 'cancelled'));

ALTER TABLE notification_deliveries
    ADD COLUMN dedupe_key text NOT NULL DEFAULT '',
    -- The rendered event, so a resumed worker can send the same message.
    ADD COLUMN payload text NOT NULL DEFAULT '',
    ADD COLUMN incident_id uuid REFERENCES incidents(id) ON DELETE SET NULL,
    ADD COLUMN attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now(),
    -- Age basis for the 24-hour delivery expiry. Owner retry moves this
    -- forward while created_at keeps the ordering/inbox identity.
    ADD COLUMN expires_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN lease_until timestamptz,
    ADD COLUMN completed_at timestamptz,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

UPDATE notification_deliveries SET expires_at = created_at + interval '24 hours';
ALTER TABLE notification_deliveries ALTER COLUMN expires_at SET DEFAULT (now() + interval '24 hours');

-- One local delivery per event/channel. Producers that hand the same logical
-- occurrence twice conflict instead of enqueueing a duplicate.
CREATE UNIQUE INDEX notification_deliveries_dedupe_idx
    ON notification_deliveries (channel_id, dedupe_key)
    WHERE dedupe_key <> '';

-- Claim order and lease recovery.
CREATE INDEX notification_deliveries_work_idx
    ON notification_deliveries (next_attempt_at, id)
    WHERE status IN ('pending', 'retrying', 'sending');

-- Health reads: newest outcome per channel.
CREATE INDEX notification_deliveries_channel_outcome_idx
    ON notification_deliveries (channel_id, completed_at DESC, id DESC);

-- Inbox dedupe keeps one row per logical occurrence/transition even when a
-- producer retries the handoff.
ALTER TABLE notifications ADD COLUMN dedupe_key text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX notifications_dedupe_idx
    ON notifications (dedupe_key)
    WHERE dedupe_key <> '';

-- +goose Down
DROP INDEX IF EXISTS notifications_dedupe_idx;
ALTER TABLE notifications DROP COLUMN IF EXISTS dedupe_key;

DROP INDEX IF EXISTS notification_deliveries_channel_outcome_idx;
DROP INDEX IF EXISTS notification_deliveries_work_idx;
DROP INDEX IF EXISTS notification_deliveries_dedupe_idx;

ALTER TABLE notification_deliveries
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS lease_until,
    DROP COLUMN IF EXISTS next_attempt_at,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS attempts,
    DROP COLUMN IF EXISTS incident_id,
    DROP COLUMN IF EXISTS payload,
    DROP COLUMN IF EXISTS dedupe_key;

ALTER TABLE notification_deliveries
    DROP CONSTRAINT notification_deliveries_status_check;

UPDATE notification_deliveries
SET status = CASE status WHEN 'delivered' THEN 'success' ELSE 'failure' END;

ALTER TABLE notification_deliveries
    ADD CONSTRAINT notification_deliveries_status_check
    CHECK (status IN ('success', 'failure'));
