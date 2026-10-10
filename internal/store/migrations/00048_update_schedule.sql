-- +goose Up
-- Persisted self-update schedule (JUS-93). A single-row table (id is pinned to
-- true) holds the operator's check / auto-apply schedule so it survives a
-- restart and can be hot-reloaded. No row means "use the environment
-- defaults" (AUTO_UPDATE, AUTO_UPDATE_INTERVAL, GOTHAM_UPDATE_CHANNEL);
-- FEATURE_UPDATES=false still removes the whole surface.
CREATE TABLE update_schedule (
    id               boolean     PRIMARY KEY DEFAULT true CHECK (id),
    check_enabled    boolean     NOT NULL,
    auto_apply       boolean     NOT NULL,
    channel          text        NOT NULL CHECK (channel IN ('stable', 'beta')),
    frequency        text        NOT NULL CHECK (frequency IN ('interval', 'daily', 'weekly')),
    interval_minutes integer     NOT NULL CHECK (interval_minutes BETWEEN 60 AND 43200),
    at_time          text        NOT NULL CHECK (at_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    weekday          smallint    NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CHECK (check_enabled OR NOT auto_apply)
);

-- +goose Down
DROP TABLE IF EXISTS update_schedule;
