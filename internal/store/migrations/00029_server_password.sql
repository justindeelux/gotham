-- +goose Up
-- Password auth for a managed node (JUS-5). The password is stored encrypted
-- at rest with the same AES-256-GCM mechanism as private keys (see keys.go);
-- NULL means no password is stored. It is never returned by any API.
ALTER TABLE servers ADD COLUMN encrypted_password text;

-- +goose Down
ALTER TABLE servers DROP COLUMN IF EXISTS encrypted_password;
