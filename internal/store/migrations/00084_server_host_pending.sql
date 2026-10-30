-- +goose Up
-- Host settings are per node (GOTHAM-150). Network, system and timezone are
-- read from the host itself; the only state the control plane keeps is the
-- unconfirmed network change, because the countdown banner has to survive a
-- control-plane restart. One row per node: a node has at most one pending
-- change. The primary key enforces that, so a second apply is refused by the
-- database instead of racing the host helper.
CREATE TABLE server_host_pending (
    server_id  uuid PRIMARY KEY REFERENCES servers (id) ON DELETE CASCADE,
    pending    jsonb       NOT NULL,
    deadline   timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- The instance-wide record described the machine the control plane runs on,
-- which is the localhost node now. Carry it over so an upgrade keeps an
-- in-flight network trial and its revert countdown instead of silently
-- dropping it.
INSERT INTO server_host_pending (server_id, pending, deadline)
SELECT s.id, i.pending_network, COALESCE(i.pending_deadline, now())
FROM instance_settings i
JOIN servers s ON s.node_id = 'localhost'
WHERE i.id = 1 AND i.pending_network IS NOT NULL
ON CONFLICT (server_id) DO NOTHING;

-- +goose Down
DROP TABLE server_host_pending;
