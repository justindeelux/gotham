-- +goose Up
-- Startup reuses this migration so late registration cannot create a second local node.
LOCK TABLE servers IN SHARE ROW EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
DECLARE
    candidates uuid[];
    candidate_names text;
BEGIN
    SELECT array_agg(id), string_agg(node_id, ', ' ORDER BY node_id)
    INTO candidates, candidate_names
    FROM servers
    WHERE node_id <> 'localhost'
      AND (btrim(ip) = '::1'
        OR btrim(CASE WHEN btrim(ip) = '' THEN node_id ELSE ip END)
           ~ '^(([a-z]+)://)?(127[.]0[.]0[.]1|localhost|\[::1\])(:[0-9]+)?$');

    IF cardinality(candidates) > 1 THEN
        RAISE EXCEPTION 'cannot identify legacy localhost node safely: candidates [%]; inspect servers and the saved agent.env, reconcile nodes pointing to the local agent, then retry', candidate_names;
    END IF;
    IF cardinality(candidates) = 1 THEN
        IF EXISTS (SELECT 1 FROM servers WHERE node_id = 'localhost') THEN
            RAISE EXCEPTION 'both legacy local node [%] and localhost exist; reconcile their workloads and remove the duplicate local agent entry before retrying', candidate_names;
        END IF;
        UPDATE servers SET node_id = 'localhost', name = 'localhost',
            ip = '127.0.0.1', team_id = NULL, updated_at = now()
        WHERE id = candidates[1];
    END IF;
END $$;
-- +goose StatementEnd
