-- +goose Up
-- Bind an enrolled node to exactly one client key pair (JUS-194). The agent
-- presents a CA-signed certificate on the gRPC agent channel; the control plane
-- records the public key fingerprint of the leaf it issued and requires every
-- later connection to prove the same key. NULL means "not bound yet": a node
-- enrolled before mutual TLS has no stored key, so the first authenticated
-- connection binds it (the upgrade path). Re-enrolling with a different key is
-- refused unless the node goes through the gated bootstrap again, which is what
-- makes rotating a node's credential invalidate the previous one.
ALTER TABLE servers ADD COLUMN agent_key_fingerprint text;

-- +goose Down
ALTER TABLE servers DROP COLUMN IF EXISTS agent_key_fingerprint;
