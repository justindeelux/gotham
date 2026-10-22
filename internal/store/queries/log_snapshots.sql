-- name: GetBoundedBuildLog :one
WITH parts AS (
  SELECT state, string_to_array(build_log, E'\n') AS lines,
    CASE WHEN right(build_log, 1) = E'\n' THEN 1 ELSE 0 END AS trailing_newline
  FROM deployments WHERE id = sqlc.arg(deployment_id) AND application_id = sqlc.arg(application_id)
), tail AS (
  SELECT state, coalesce(array_to_string(lines[greatest(1, cardinality(lines) - sqlc.arg(tail)::integer + 1 - trailing_newline):cardinality(lines)], E'\n'), '') AS log
  FROM parts
)
SELECT state, left(log, sqlc.arg(max_bytes)::integer + 1)::text AS log,
  (octet_length(log) > sqlc.arg(max_bytes)::integer)::boolean AS more
FROM tail;
