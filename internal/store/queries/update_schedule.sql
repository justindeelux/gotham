-- name: GetUpdateSchedule :one
SELECT check_enabled, auto_apply, channel, frequency, interval_minutes, at_time, weekday, updated_at
FROM update_schedule
WHERE id = true;

-- name: UpsertUpdateSchedule :one
INSERT INTO update_schedule (id, check_enabled, auto_apply, channel, frequency, interval_minutes, at_time, weekday)
VALUES (true, $1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO UPDATE SET
    check_enabled = EXCLUDED.check_enabled,
    auto_apply = EXCLUDED.auto_apply,
    channel = EXCLUDED.channel,
    frequency = EXCLUDED.frequency,
    interval_minutes = EXCLUDED.interval_minutes,
    at_time = EXCLUDED.at_time,
    weekday = EXCLUDED.weekday,
    updated_at = now()
RETURNING check_enabled, auto_apply, channel, frequency, interval_minutes, at_time, weekday, updated_at;
