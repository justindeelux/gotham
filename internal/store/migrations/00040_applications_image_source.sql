-- +goose Up
-- Docker image sources (GS-9): the prebuilt reference to pull, and one
-- optional private-registry credential per application. The password travels
-- sealed (AES-256-GCM, like other secrets) and is never returned by the API;
-- the username is stored plainly and is equally never returned or logged.
-- The resolved digest of each pull is recorded on the deployment row, so no
-- column for it is needed here.
ALTER TABLE applications ADD COLUMN image_ref text NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN registry_username text NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN registry_password_ciphertext text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE applications DROP COLUMN IF EXISTS registry_password_ciphertext;
ALTER TABLE applications DROP COLUMN IF EXISTS registry_username;
ALTER TABLE applications DROP COLUMN IF EXISTS image_ref;
