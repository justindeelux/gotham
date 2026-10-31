-- +goose Up
-- Cleanup threshold (GOTHAM-166): when a node's disk usage stays above the
-- percent, Gotham cleans the chosen categories once without waiting for the
-- owner. Stored like the cleanup retention settings so the same audit trail
-- records every change. The category list defaults to the safe tier.
ALTER TABLE instance_settings
    ADD COLUMN cleanup_threshold_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN cleanup_threshold_percent integer NOT NULL DEFAULT 85
        CHECK (cleanup_threshold_percent BETWEEN 50 AND 99),
    ADD COLUMN cleanup_threshold_categories text[] NOT NULL
        DEFAULT ARRAY['build_cache', 'dangling_images', 'stopped_containers', 'journal', 'package_cache']::text[],
    ADD COLUMN cleanup_threshold_min_age_hours integer NOT NULL DEFAULT 24
        CHECK (cleanup_threshold_min_age_hours BETWEEN 1 AND 8760),
    ADD COLUMN cleanup_threshold_cooldown_hours integer NOT NULL DEFAULT 6
        CHECK (cleanup_threshold_cooldown_hours BETWEEN 1 AND 168);

-- +goose Down
ALTER TABLE instance_settings
    DROP COLUMN IF EXISTS cleanup_threshold_cooldown_hours,
    DROP COLUMN IF EXISTS cleanup_threshold_min_age_hours,
    DROP COLUMN IF EXISTS cleanup_threshold_categories,
    DROP COLUMN IF EXISTS cleanup_threshold_percent,
    DROP COLUMN IF EXISTS cleanup_threshold_enabled;
