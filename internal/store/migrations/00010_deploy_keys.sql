-- +goose Up
-- Application SSH deploy keys (Phase 4, BE-4.4b). One row per application that
-- clones a private repository over SSH. It records the key registered on the
-- Git host (provider_key_id) plus the public half Gotham registered — public
-- key and SHA-256 fingerprint — so the host-side key can be recognised and
-- removed later without ever reading the private half again. The private key
-- itself lives in private_keys, sealed exactly like a node SSH key
-- (providers.SealSecret / servers.EncryptKey share one AES-256-GCM format).
CREATE TABLE application_deploy_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    private_key_id uuid NOT NULL REFERENCES private_keys(id) ON DELETE CASCADE,
    provider text NOT NULL DEFAULT '',
    repo text NOT NULL DEFAULT '',
    provider_key_id text NOT NULL DEFAULT '',
    fingerprint text NOT NULL DEFAULT '',
    public_key text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX application_deploy_keys_app_idx ON application_deploy_keys (application_id);

-- +goose Down
DROP TABLE IF EXISTS application_deploy_keys;
