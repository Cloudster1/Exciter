-- +goose Up
-- The trace graph (#211), as docs/api-contract-m11.md agrees: how many of a requirement's links lead to requirements
-- the person asking may not see, so the graph counts them without naming them.

-- +goose StatementBegin
-- For each requirement the person asking may see, its live links of the types given (every type when none is), in
-- the direction asked for it, whose other end they may not see. A type that reads the same both ways counts either
-- way, and an end in the trash is not counted. It counts for the person asking alone, never for one named.
CREATE FUNCTION trace_hidden_links(ids uuid[], directions text[], types uuid[]) RETURNS TABLE (id uuid, hidden integer)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT a.id, count(*)::int
    FROM unnest(ids, directions) AS a (id, direction)
    JOIN trace_link l ON l.deleted_at IS NULL AND (l.source_id = a.id OR l.target_id = a.id)
    JOIN link_type lt ON lt.id = l.type_id
    JOIN requirement o ON o.id = CASE WHEN l.source_id = a.id THEN l.target_id ELSE l.source_id END
    WHERE (types IS NULL OR l.type_id = ANY (types))
      AND (a.direction = 'both' OR lt.forward = lt.backward
           OR (a.direction = 'outgoing' AND l.source_id = a.id) OR (a.direction = 'incoming' AND l.target_id = a.id))
      AND o.trashed_at IS NULL
      AND perm_requirement_viewable(a.id, current_actor_id())
      AND NOT perm_requirement_viewable(o.id, current_actor_id())
    GROUP BY a.id
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION trace_hidden_links(uuid[], text[], uuid[]);
