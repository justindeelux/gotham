-- +goose Up
-- Backup integrity (GOTHAM-132).
--
-- The checksum is computed over the stored artifact (the gzip bytes, before
-- any provider transformation) while the run is written, so a restore can
-- detect a truncated upload, a bucket lifecycle rule or disk corruption before
-- the resource is stopped. Existing rows stay NULL: they were written before
-- this column existed and keep restoring, labelled "not verified" in the UI.
ALTER TABLE backups
    ADD COLUMN checksum_sha256 text,
    ADD COLUMN checksum_algo text;

-- +goose Down
ALTER TABLE backups
    DROP COLUMN IF EXISTS checksum_algo,
    DROP COLUMN IF EXISTS checksum_sha256;
