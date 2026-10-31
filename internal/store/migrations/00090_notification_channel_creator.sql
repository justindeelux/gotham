-- +goose Up
-- Track who created a notification channel. A channel sealed before
-- allow_private existed resolves its private-target trust from this creator's
-- current role; rows without a creator stay refused.
ALTER TABLE notification_channels
    ADD COLUMN created_by uuid REFERENCES users(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE notification_channels DROP COLUMN IF EXISTS created_by;
