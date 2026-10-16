-- +goose Up
-- Notification delivery history (JUS-195). One row per delivery attempt a
-- channel made: a dispatched deploy/backup event or an operator's send-test.
-- The row carries only non-secret facts — the event type, the terminal status
-- and a bounded error summary — so a redacted failure reason is visible in the
-- settings UI without storing a webhook URL, bot token or SMTP password.
--
-- Rows are scoped to their channel (and so to the channel's team) and cascade
-- away when the channel is deleted. Retention is bounded per channel by the
-- notifications repository, which prunes to the newest N rows on every write.
CREATE TABLE notification_deliveries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id uuid NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    -- The delivered event key (deploy_success, backup_failure, …) or 'test'
    -- for a send-test attempt.
    event_type text NOT NULL,
    status text NOT NULL,
    -- A classified, bounded failure reason; empty on success. Never a
    -- credential or raw remote content.
    error_summary text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notification_deliveries_status_check CHECK (status IN ('success', 'failure'))
);

-- Lists one channel's newest attempts (the history panel read) and backs the
-- per-channel retention prune.
CREATE INDEX notification_deliveries_channel_idx ON notification_deliveries (channel_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS notification_deliveries;
