-- +goose Up
-- Database backups (Phase 5, BE-5.2). A backup is one run of the
-- per-engine dump job: a temporary container on the database's node writes
-- the dump to stdout, the control plane compresses it and stores the bytes
-- either locally or in an S3-compatible bucket. `location` records where the
-- bytes ended up ("s3://bucket/key" or "file:///path"), `size` their
-- compressed length.
--
--	running → completed | failed
--
-- backup_targets is the destination configuration: endpoint, bucket and
-- prefix in the clear, credentials sealed with AES-256-GCM
-- (providers.SealSecret) in backup_target_secrets — never in this table and
-- never in an API response.
CREATE TABLE backup_targets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    kind text NOT NULL DEFAULT 's3',
    endpoint text NOT NULL DEFAULT '',
    region text NOT NULL DEFAULT 'us-east-1',
    bucket text NOT NULL DEFAULT '',
    prefix text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT backup_targets_kind_check CHECK (kind IN ('s3', 'local')),
    CONSTRAINT backup_targets_name_check CHECK (name <> '')
);

CREATE UNIQUE INDEX backup_targets_user_name_idx ON backup_targets (user_id, name);

-- Sealed credentials of a target. Keys in use: access_key and secret_key.
-- Ciphertext is base64(nonce||ciphertext) sealed with providers.SealSecret;
-- plaintext exists only while a job builds its storage client.
CREATE TABLE backup_target_secrets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id uuid NOT NULL REFERENCES backup_targets(id) ON DELETE CASCADE,
    key text NOT NULL,
    ciphertext text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT backup_target_secrets_key_unique UNIQUE (target_id, key)
);

CREATE INDEX backup_target_secrets_target_idx ON backup_target_secrets (target_id, key);

-- Schedules for automatic backups. next_run_at is denormalised from the cron
-- expression on every write so the scheduler is a single due-time query:
-- enabled AND next_run_at <= now().
CREATE TABLE backup_schedules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id uuid NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    cron text NOT NULL,
    target_id uuid REFERENCES backup_targets(id) ON DELETE SET NULL,
    enabled boolean NOT NULL DEFAULT true,
    last_run_at timestamptz,
    next_run_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT backup_schedules_cron_check CHECK (cron <> '')
);

CREATE INDEX backup_schedules_due_idx ON backup_schedules (next_run_at)
    WHERE enabled;
CREATE INDEX backup_schedules_database_idx ON backup_schedules (database_id);

-- One row per backup run. schedule_id is NULL for manual backups and set
-- when the internal cron scheduler triggered the job; target_id is NULL when
-- the bytes were kept locally.
CREATE TABLE backups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id uuid NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    schedule_id uuid REFERENCES backup_schedules(id) ON DELETE SET NULL,
    type text NOT NULL DEFAULT 'manual',
    status text NOT NULL DEFAULT 'running',
    size bigint NOT NULL DEFAULT 0,
    location text NOT NULL DEFAULT '',
    target_id uuid REFERENCES backup_targets(id) ON DELETE SET NULL,
    container_id text NOT NULL DEFAULT '',
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    CONSTRAINT backups_type_check CHECK (type IN ('manual', 'scheduled')),
    CONSTRAINT backups_status_check CHECK (status IN ('running', 'completed', 'failed')),
    CONSTRAINT backups_size_check CHECK (size >= 0)
);

CREATE INDEX backups_database_idx ON backups (database_id, created_at DESC);
CREATE INDEX backups_schedule_idx ON backups (schedule_id);

-- +goose Down
DROP TABLE IF EXISTS backups;
DROP TABLE IF EXISTS backup_schedules;
DROP TABLE IF EXISTS backup_target_secrets;
DROP TABLE IF EXISTS backup_targets;
