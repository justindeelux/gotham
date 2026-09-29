-- +goose Up
-- Notification channels (Phase 8, BE-8.3). One row per configured transport;
-- a channel belongs to exactly one team and either covers the whole team
-- (resource_type NULL) or overrides the team config for one resource
-- (application or database).
--
-- config holds the whole transport configuration, sealed with AES-256-GCM
-- (providers.SealSecret with GOTHAM_SECRET_KEY): webhook URLs, bot tokens and
-- SMTP passwords never live in the clear, are never logged and never returned
-- by the API. The read DTO carries a redacted view instead (see the
-- notifications package).
--
-- events lists the event keys the channel receives; a channel whose list does
-- not name the event is skipped by the dispatcher. The default subscribes a
-- new channel to all four terminal events.
CREATE TABLE notification_channels (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name text NOT NULL,
    kind text NOT NULL,
    config text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    resource_type text,
    resource_id uuid,
    events text[] NOT NULL DEFAULT '{deploy_success,deploy_failure,backup_success,backup_failure}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notification_channels_kind_check CHECK (kind IN ('discord', 'slack', 'telegram', 'email')),
    -- A channel is either team-wide (both NULL) or a resource override (both
    -- set); one half alone can never resolve to an event.
    CONSTRAINT notification_channels_resource_check CHECK (
        (resource_type IS NULL AND resource_id IS NULL)
        OR (resource_type IN ('application', 'database') AND resource_id IS NOT NULL)
    )
);

-- Lists the channels of one team (the settings surface).
CREATE INDEX notification_channels_team_idx ON notification_channels (team_id, created_at ASC);

-- The dispatcher read: enabled channels of a team, team-wide plus the ones
-- scoped to the event's resource.
CREATE INDEX notification_channels_scope_idx ON notification_channels (team_id, resource_type, resource_id);

-- +goose Down
DROP TABLE IF EXISTS notification_channels;
