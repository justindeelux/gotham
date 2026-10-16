-- +goose Up
-- Serialize identity changes and prevent new references while checking the placeholder.
LOCK TABLE servers IN SHARE ROW EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
DECLARE
    candidates uuid[] := '{}';
    candidate_names text[] := '{}';
    candidate record;
    address text;
    parsed_address inet;
    placeholder uuid;
    reference record;
    has_reference boolean;
BEGIN
    FOR candidate IN SELECT id, node_id, ip FROM servers
        WHERE node_id <> 'localhost' AND btrim(ip) <> ''
          -- SSH loopback targets may be remote tunnels, not the local agent.
          AND os IS NOT NULL AND ssh_user = ''
          AND ssh_key_id IS NULL AND encrypted_password IS NULL
          -- Registration derives IP literals from the agent-chosen node ID.
          AND btrim(ip) <> btrim(node_id)
    LOOP
        address := regexp_replace(lower(btrim(candidate.ip)), '^[a-z]+://', '');
        IF address ~ '^\[[^]]+\](:[0-9]+)?$' THEN
            address := substring(address FROM '^\[([^]]+)\]');
        ELSIF address ~ '^[^:]+:[0-9]+$' THEN
            address := split_part(address, ':', 1);
        END IF;
        IF address = 'localhost' THEN
            address := '127.0.0.1';
        END IF;
        BEGIN
            parsed_address := address::inet;
        EXCEPTION WHEN invalid_text_representation THEN
            CONTINUE;
        END;
        IF parsed_address <<= '127.0.0.0/8'::inet
            OR parsed_address = '::1'::inet
            OR parsed_address <<= '::ffff:127.0.0.0/104'::inet THEN
            candidates := array_append(candidates, candidate.id);
            candidate_names := array_append(candidate_names, candidate.node_id);
        END IF;
    END LOOP;

    IF cardinality(candidates) > 1 THEN
        RAISE EXCEPTION 'cannot identify legacy localhost node safely: candidates [%]; inspect servers and the saved agent.env, reconcile nodes pointing to the local agent, then retry', array_to_string(candidate_names, ', ');
    END IF;
    IF cardinality(candidates) = 1 THEN
        IF EXISTS (SELECT 1 FROM servers WHERE node_id = 'localhost') THEN
            SELECT id INTO placeholder FROM servers
            WHERE node_id = 'localhost' AND status = 'pending' AND last_seen IS NULL
              AND os IS NULL AND docker_version IS NULL AND arch IS NULL
              AND total_mem IS NULL AND total_disk IS NULL
              AND cpu_usage IS NULL AND mem_usage IS NULL AND disk_usage IS NULL
              AND container_count IS NULL AND team_id IS NULL AND ssh_user = ''
              AND ssh_key_id IS NULL AND encrypted_password IS NULL
              AND host_key_fingerprint IS NULL
            FOR UPDATE;
            IF placeholder IS NULL THEN
                RAISE EXCEPTION 'both legacy local node [%] and a registered localhost exist; reconcile their workloads before retrying', array_to_string(candidate_names, ', ');
            END IF;
            -- Check every FK, including CASCADE references that DELETE would silently lose.
            FOR reference IN
                SELECT c.conrelid::regclass AS relation, a.attname AS column_name
                FROM pg_constraint c
                JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
                WHERE c.contype = 'f' AND c.confrelid = 'servers'::regclass
            LOOP
                EXECUTE format('SELECT EXISTS (SELECT 1 FROM %s WHERE %I = $1)',
                    reference.relation, reference.column_name)
                INTO has_reference USING placeholder;
                IF has_reference THEN
                    RAISE EXCEPTION 'localhost has references in %; reconcile their workloads before retrying', reference.relation;
                END IF;
            END LOOP;
            DELETE FROM servers WHERE id = placeholder;
        END IF;
        UPDATE servers SET node_id = 'localhost', name = 'localhost',
            ip = '127.0.0.1', team_id = NULL, updated_at = now()
        WHERE id = candidates[1];
    END IF;
END $$;
-- +goose StatementEnd
