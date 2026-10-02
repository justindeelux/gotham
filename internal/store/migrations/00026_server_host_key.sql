-- +goose Up
-- TOFU-pinned SSH host key for a managed node (FX-4). The fingerprint is the
-- OpenSSH SHA256 form ("SHA256:..."). NULL means the node has never been
-- validated; a non-NULL value is the host key every later validation must
-- present. An operator can clear it to re-pin after a legitimate key rotation.
ALTER TABLE servers ADD COLUMN host_key_fingerprint text;

-- +goose Down
ALTER TABLE servers DROP COLUMN IF EXISTS host_key_fingerprint;
