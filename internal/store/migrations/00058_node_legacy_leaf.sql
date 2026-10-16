-- +goose Up
-- Whether the last node certificate the control plane accepted for this node was
-- a legacy, serverAuth-only leaf, or a modern dual-EKU leaf (JUS-194 M5).
-- NULL means "not observed yet". The gateway records it, so the operator gets a
-- startup warning naming how many nodes still need to renew before the legacy
-- leaf sunset and can find them with a query.
ALTER TABLE servers ADD COLUMN agent_legacy_leaf boolean;

-- +goose Down
ALTER TABLE servers DROP COLUMN IF EXISTS agent_legacy_leaf;
