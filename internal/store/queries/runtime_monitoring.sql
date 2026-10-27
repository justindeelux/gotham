-- name: GetRuntimeMonitorSettings :one
SELECT enabled, grace_seconds, desired_state, grace_until
FROM runtime_monitor_settings
WHERE resource_type = $1 AND resource_id = $2;

-- name: UpsertRuntimeMonitorSettings :exec
INSERT INTO runtime_monitor_settings (resource_type, resource_id, enabled, grace_seconds)
VALUES ($1, $2, $3, $4)
ON CONFLICT (resource_type, resource_id) DO UPDATE
SET enabled = EXCLUDED.enabled,
    grace_seconds = EXCLUDED.grace_seconds,
    updated_at = now();

-- name: SetRuntimeMonitorDesiredState :exec
INSERT INTO runtime_monitor_settings (resource_type, resource_id, desired_state, grace_until)
VALUES ($1, $2, $3, $4)
ON CONFLICT (resource_type, resource_id) DO UPDATE
SET desired_state = EXCLUDED.desired_state,
    grace_until = EXCLUDED.grace_until,
    updated_at = now();

-- name: ListRuntimeMonitorApplications :many
-- One node's applications with their monitoring settings and latest
-- deployment. deployment_state is empty when the application never deployed.
SELECT a.id,
       a.name,
       a.server_id,
       COALESCE(e.project_id, '00000000-0000-0000-0000-000000000000'::uuid) AS project_id,
       a.source_type,
       a.provider,
       COALESCE(s.enabled, true) AS enabled,
       COALESCE(s.grace_seconds, 120) AS grace_seconds,
       COALESCE(s.desired_state, 'running') AS desired_state,
       s.grace_until,
       COALESCE(d.state, '') AS deployment_state,
       COALESCE(d.started_at, d.created_at) AS deployment_started_at,
       d.finished_at AS deployment_finished_at
FROM applications a
LEFT JOIN environments e ON e.id = a.environment_id
LEFT JOIN runtime_monitor_settings s
    ON s.resource_type = 'application' AND s.resource_id = a.id
LEFT JOIN LATERAL (
    SELECT state, started_at, finished_at, created_at
    FROM deployments
    WHERE application_id = a.id
    ORDER BY created_at DESC, id DESC
    LIMIT 1
) d ON true
WHERE a.server_id = $1;

-- name: ListRuntimeMonitorDatabases :many
-- One node's live databases with their monitoring settings. The database
-- status is the existing desired-state evidence for a stop.
SELECT d.id,
       d.name,
       d.server_id,
       COALESCE(e.project_id, '00000000-0000-0000-0000-000000000000'::uuid) AS project_id,
       d.status,
       COALESCE(s.enabled, true) AS enabled,
       COALESCE(s.grace_seconds, 120) AS grace_seconds,
       COALESCE(s.desired_state, 'running') AS desired_state,
       s.grace_until
FROM databases d
LEFT JOIN environments e ON e.id = d.environment_id
LEFT JOIN runtime_monitor_settings s
    ON s.resource_type = 'database' AND s.resource_id = d.id
WHERE d.server_id = $1 AND d.deleted_at IS NULL;

-- name: ListRuntimeMonitorReplicaStates :many
SELECT * FROM runtime_monitor_replicas
WHERE resource_type = $1 AND resource_id = $2
ORDER BY service, replica;

-- name: ListRuntimeMonitorReplicaStatesByResources :many
SELECT * FROM runtime_monitor_replicas
WHERE resource_type = $1
  AND resource_id = ANY(sqlc.arg(resource_ids)::uuid[])
ORDER BY resource_id, service, replica;

-- name: ListRuntimeMonitorReplicaStatesForNode :many
SELECT * FROM runtime_monitor_replicas
WHERE node_id = $1
ORDER BY resource_type, resource_id, service, replica;

-- name: UpsertRuntimeMonitorReplicaState :exec
INSERT INTO runtime_monitor_replicas (
    node_id, resource_type, resource_id, service, replica, container_id, deployment_id,
    state, health, reason, coverage, restart_count, restart_events, oom_container,
    oom_at, observed_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, now())
ON CONFLICT (node_id, resource_type, resource_id, service, replica) DO UPDATE
SET container_id = EXCLUDED.container_id,
    deployment_id = EXCLUDED.deployment_id,
    state = EXCLUDED.state,
    health = EXCLUDED.health,
    reason = EXCLUDED.reason,
    coverage = EXCLUDED.coverage,
    restart_count = EXCLUDED.restart_count,
    restart_events = EXCLUDED.restart_events,
    oom_container = EXCLUDED.oom_container,
    oom_at = EXCLUDED.oom_at,
    observed_at = EXCLUDED.observed_at,
    updated_at = now();

-- name: DeleteRuntimeMonitorReplicaState :exec
DELETE FROM runtime_monitor_replicas
WHERE node_id = $1 AND resource_type = $2 AND resource_id = $3
  AND service = $4 AND replica = $5;

-- name: DeleteRuntimeMonitorStateForNode :exec
DELETE FROM runtime_monitor_replicas WHERE node_id = $1;

-- name: DeleteRuntimeMonitorStateForResource :exec
DELETE FROM runtime_monitor_replicas WHERE resource_type = $1 AND resource_id = $2;

-- name: DeleteRuntimeMonitorStateOnOtherNodes :exec
DELETE FROM runtime_monitor_replicas
WHERE resource_type = $1 AND resource_id = $2 AND node_id <> $3;

-- name: DeleteRuntimeMonitorSettings :exec
DELETE FROM runtime_monitor_settings WHERE resource_type = $1 AND resource_id = $2;

-- name: ListActiveIncidentsByResources :many
SELECT * FROM incidents
WHERE resource_type = $1
  AND resource_id = ANY(sqlc.arg(resource_ids)::uuid[])
  AND lifecycle <> 'resolved'
ORDER BY created_at ASC;

-- name: ListActiveIncidentsForNode :many
SELECT * FROM incidents
WHERE node_id = $1 AND lifecycle <> 'resolved';
