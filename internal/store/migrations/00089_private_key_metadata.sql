-- +goose Up
-- Key manager metadata (GOTHAM-168): private_keys gains the key type, an
-- optional owner note and the OpenSSH authorized_keys line for the public
-- half. Existing rows stay NULL until they are read, because type and public
-- key can only be derived by decrypting the stored PEM with the instance
-- secret, which SQL cannot do.
ALTER TABLE private_keys
    ADD COLUMN type text,
    ADD COLUMN description text,
    ADD COLUMN public_key text;

ALTER TABLE private_keys
    ADD CONSTRAINT private_keys_type_check
        CHECK (type IS NULL OR type IN ('ed25519', 'rsa', 'ecdsa'));

-- +goose Down
ALTER TABLE private_keys
    DROP CONSTRAINT IF EXISTS private_keys_type_check,
    DROP COLUMN IF EXISTS public_key,
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS type;
