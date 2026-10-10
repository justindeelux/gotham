-- +goose Up
-- Instance settings (JUS-92): the operator-editable configuration of the
-- Gotham instance itself. One singleton row holds the desired state; nullable
-- general columns mean "not set, use the built-in default". An unconfirmed
-- network change is kept in pending_network (with its deadline) so it survives
-- a control-plane restart and can be reconciled against the host revert timer.
CREATE TABLE instance_settings (
    id                smallint PRIMARY KEY CHECK (id = 1),
    control_plane_url text,
    instance_name     text,
    timezone          text,
    dns_servers       text[]      NOT NULL DEFAULT '{}',
    ipv4_mode         text        NOT NULL DEFAULT 'dhcp' CHECK (ipv4_mode IN ('dhcp', 'static')),
    ipv4_address      text        NOT NULL DEFAULT '',
    ipv4_gateway      text        NOT NULL DEFAULT '',
    ipv6_enabled      boolean     NOT NULL DEFAULT true,
    ipv6_mode         text        NOT NULL DEFAULT 'dhcp' CHECK (ipv6_mode IN ('dhcp', 'static')),
    ipv6_address      text        NOT NULL DEFAULT '',
    ipv6_gateway      text        NOT NULL DEFAULT '',
    hostname          text        NOT NULL DEFAULT '',
    ntp_enabled       boolean     NOT NULL DEFAULT true,
    ntp_servers       text[]      NOT NULL DEFAULT '{}',
    pending_network   jsonb,
    pending_deadline  timestamptz,
    updated_at        timestamptz NOT NULL DEFAULT now()
);

INSERT INTO instance_settings (id) VALUES (1);

-- Append-only audit trail of every successful settings write.
CREATE TABLE instance_settings_audit (
    id         bigserial PRIMARY KEY,
    actor_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    section    text        NOT NULL,
    changes    jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX instance_settings_audit_created_idx ON instance_settings_audit (created_at DESC);

-- +goose Down
DROP TABLE instance_settings_audit;
DROP TABLE instance_settings;
