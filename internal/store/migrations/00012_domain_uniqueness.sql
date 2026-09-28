-- +goose Up
-- Per-node normalized-domain uniqueness (BE-6.1 F6): two applications on the
-- same node must not serve the same hostname. The migration is data-safe for
-- existing rows: it normalizes stored casing, then disables - never deletes or
-- reassigns - EVERY binding involved in a legacy conflict. No winner is
-- chosen: row age does not prove domain ownership, so all conflicting
-- bindings fail closed and keep their stored value until an owner sets a
-- different valid domain (the partial unique index then admits the changed
-- binding). The disabled state is surfaced as a per-application sync
-- diagnostic.
ALTER TABLE applications ADD COLUMN base_domain_disabled boolean NOT NULL DEFAULT false;

UPDATE applications
SET base_domain = lower(base_domain)
WHERE base_domain <> lower(base_domain);

WITH conflicts AS (
    SELECT id,
           count(*) OVER (PARTITION BY server_id, lower(base_domain)) AS bindings
    FROM applications
    WHERE base_domain <> '' AND server_id IS NOT NULL
)
UPDATE applications
SET base_domain_disabled = true
FROM conflicts
WHERE applications.id = conflicts.id AND conflicts.bindings > 1;

CREATE UNIQUE INDEX applications_server_domain_idx
    ON applications (server_id, lower(base_domain))
    WHERE base_domain <> '' AND NOT base_domain_disabled;

-- +goose Down
DROP INDEX IF EXISTS applications_server_domain_idx;
ALTER TABLE applications DROP COLUMN IF EXISTS base_domain_disabled;
