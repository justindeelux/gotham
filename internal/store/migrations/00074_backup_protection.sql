-- +goose Up
-- Backup protection uses persisted scheduling evidence instead of polling
-- agents: the observer compares each enabled schedule's expected slot with the
-- newest successful run for the same database and target.
--
-- last_slot_at records the slot the scheduler last advanced past. next_run_at
-- is still the next expected slot; it stays in the past when the scheduler
-- stops creating jobs, so both values together prove a missed slot across a
-- control-plane restart.
ALTER TABLE backup_schedules
    ADD COLUMN last_slot_at timestamptz,
    ADD COLUMN max_run_seconds integer NOT NULL DEFAULT 7200
        CONSTRAINT backup_schedules_max_run_check
        CHECK (max_run_seconds BETWEEN 900 AND 86400);

-- Existing schedules have at least one fired slot; the firing time is a
-- conservative upper bound for it. A run that succeeded at or after that
-- instant still counts, and a missed slot stays visible until a later success.
UPDATE backup_schedules
SET last_slot_at = last_run_at
WHERE last_run_at IS NOT NULL;

-- +goose Down
ALTER TABLE backup_schedules
    DROP COLUMN IF EXISTS max_run_seconds,
    DROP COLUMN IF EXISTS last_slot_at;
