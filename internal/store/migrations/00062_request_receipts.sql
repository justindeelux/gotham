-- +goose Up
-- No foreign keys: deletion must not erase the identity of an accepted request.
CREATE TABLE IF NOT EXISTS request_receipts (
    principal_id uuid NOT NULL,
    team_id uuid NOT NULL,
    operation text NOT NULL,
    target_id uuid NOT NULL,
    request_key varchar(128) NOT NULL CHECK (request_key ~ '^[A-Za-z0-9._:-]{1,128}$'),
    fingerprint text NOT NULL,
    resource_id uuid NOT NULL,
    related_id uuid NOT NULL,
    accepted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    webhook_outcome text NOT NULL DEFAULT 'not_confirmed'
        CHECK (webhook_outcome IN ('not_confirmed', 'none', 'installed', 'warning')),
    PRIMARY KEY (principal_id, team_id, operation, target_id, request_key)
);
CREATE INDEX IF NOT EXISTS request_receipts_expiry_idx ON request_receipts (accepted_at);
-- Forward-only: rollback disables opt-in handlers and retains this table.
