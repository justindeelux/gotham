-- Domain reachability monitoring (GOTHAM-87).

-- name: GetDomainMonitorSettings :one
SELECT * FROM domain_monitor_settings WHERE domain_id = $1;

-- name: UpsertDomainMonitorSettings :one
INSERT INTO domain_monitor_settings (
    domain_id, enabled, path, expected_min, expected_max, timeout_seconds, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (domain_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    path = EXCLUDED.path,
    expected_min = EXCLUDED.expected_min,
    expected_max = EXCLUDED.expected_max,
    timeout_seconds = EXCLUDED.timeout_seconds,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetDomainMonitorState :one
SELECT * FROM domain_monitor_state WHERE domain_id = $1;

-- name: UpsertDomainMonitorState :one
INSERT INTO domain_monitor_state (
    domain_id, consecutive_failures, consecutive_successes, last_outcome,
    last_reason, last_status, last_latency_ms, last_checked_at,
    last_completed_at, last_success_at, last_failure_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (domain_id) DO UPDATE SET
    consecutive_failures = EXCLUDED.consecutive_failures,
    consecutive_successes = EXCLUDED.consecutive_successes,
    last_outcome = EXCLUDED.last_outcome,
    last_reason = EXCLUDED.last_reason,
    last_status = EXCLUDED.last_status,
    last_latency_ms = EXCLUDED.last_latency_ms,
    last_checked_at = EXCLUDED.last_checked_at,
    last_completed_at = EXCLUDED.last_completed_at,
    last_success_at = EXCLUDED.last_success_at,
    last_failure_at = EXCLUDED.last_failure_at,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: InsertDomainProbeResult :exec
INSERT INTO domain_probe_results (
    domain_id, application_id, observed_at, interval_start, outcome,
    reason, status_code, latency_ms, valid_seconds
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListDomainProbeResults :many
SELECT * FROM domain_probe_results
WHERE domain_id = $1 AND observed_at >= $2
ORDER BY observed_at DESC
LIMIT $3;

-- name: DeleteDomainProbeResultsBefore :execrows
DELETE FROM domain_probe_results WHERE observed_at < $1;

-- name: ListDomainProbeTargets :many
SELECT
    d.id,
    d.application_id,
    d.domain,
    d.protocol,
    d.created_at,
    d.updated_at AS domain_updated_at,
    a.name AS application_name,
    a.server_id,
    e.project_id,
    COALESCE(s.enabled, true) AS enabled,
    COALESCE(s.path, '/') AS path,
    COALESCE(s.expected_min, 200) AS expected_min,
    COALESCE(s.expected_max, 399) AS expected_max,
    COALESCE(s.timeout_seconds, 5) AS timeout_seconds,
    s.updated_at AS settings_updated_at,
    COALESCE(dep.state, '') AS deployment_state,
    dep.started_at AS deployment_started_at,
    dep.finished_at AS deployment_finished_at
FROM application_domains d
JOIN applications a ON a.id = d.application_id
JOIN environments e ON e.id = a.environment_id
LEFT JOIN domain_monitor_settings s ON s.domain_id = d.id
LEFT JOIN LATERAL (
    SELECT state, started_at, finished_at
    FROM deployments
    WHERE application_id = a.id
    ORDER BY created_at DESC
    LIMIT 1
) dep ON true
WHERE d.disabled = false
ORDER BY d.id;

-- name: GetDomainProbeTarget :one
SELECT
    d.id,
    d.application_id,
    d.domain,
    d.protocol,
    d.created_at,
    d.updated_at AS domain_updated_at,
    a.name AS application_name,
    a.server_id,
    e.project_id,
    COALESCE(s.enabled, true) AS enabled,
    COALESCE(s.path, '/') AS path,
    COALESCE(s.expected_min, 200) AS expected_min,
    COALESCE(s.expected_max, 399) AS expected_max,
    COALESCE(s.timeout_seconds, 5) AS timeout_seconds,
    s.updated_at AS settings_updated_at,
    COALESCE(dep.state, '') AS deployment_state,
    dep.started_at AS deployment_started_at,
    dep.finished_at AS deployment_finished_at
FROM application_domains d
JOIN applications a ON a.id = d.application_id
JOIN environments e ON e.id = a.environment_id
LEFT JOIN domain_monitor_settings s ON s.domain_id = d.id
LEFT JOIN LATERAL (
    SELECT state, started_at, finished_at
    FROM deployments
    WHERE application_id = a.id
    ORDER BY created_at DESC
    LIMIT 1
) dep ON true
WHERE d.application_id = $1 AND d.id = $2;

-- name: ListActiveDomainIncidents :many
SELECT id, identity_key, resource_id FROM incidents
WHERE lifecycle <> 'resolved' AND resource_type = 'domain';

-- name: ResetDomainMonitorStateCompletion :exec
UPDATE domain_monitor_state
SET consecutive_failures = 0,
    consecutive_successes = 0,
    last_completed_at = NULL,
    updated_at = $2
WHERE domain_id = $1;
