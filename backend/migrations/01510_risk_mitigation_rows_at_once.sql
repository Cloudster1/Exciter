-- +goose Up
-- The FMEA table read each risk's mitigations one risk at a time and worked out a source's coverage once for every
-- risk it mitigates, which ran a busy server past the request deadline with a few hundred links (#370). The same
-- rows now come from one pass over the links, with each source's coverage worked out once.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION risk_mitigation_rows(targets uuid[])
    RETURNS TABLE (risk_id uuid, link_id uuid, source_id uuid, source_project text, source_number integer, source_title text,
                   visible boolean, implemented text, verified text, suspect boolean, in_effect boolean)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp ROWS 100
AS $$
    WITH link AS (
        SELECT l.target_id, l.id, l.source_id
        FROM trace_link l
        JOIN link_type lt ON lt.id = l.type_id AND lt.counts = 'mitigation'
        WHERE l.target_id = ANY (targets) AND l.deleted_at IS NULL
    ), coverage AS MATERIALIZED (
        SELECT d.source_id, requirement_coverage(d.source_id, 'implemented') AS implemented,
               requirement_coverage(d.source_id, 'verified') AS verified
        FROM (SELECT DISTINCT source_id FROM link) d
    )
    SELECT l.target_id, l.id, l.source_id, sp.key, s.number, s.title, perm_project_holds(s.project_id, current_actor_id(), 'view'),
           c.implemented, c.verified, COALESCE(st.suspect, false),
           s.trashed_at IS NULL AND NOT COALESCE(st.suspect, false)
           AND c.implemented IN ('full', 'na') AND c.verified IN ('full', 'na')
    FROM link l
    JOIN coverage c ON c.source_id = l.source_id
    JOIN requirement s ON s.id = l.source_id
    JOIN project sp ON sp.id = s.project_id
    LEFT JOIN trace_link_state st ON st.id = l.id
    ORDER BY l.target_id, sp.key, s.number
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION risk_mitigation_rows(targets uuid[])
    RETURNS TABLE (risk_id uuid, link_id uuid, source_id uuid, source_project text, source_number integer, source_title text,
                   visible boolean, implemented text, verified text, suspect boolean, in_effect boolean)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp ROWS 100
AS $$
    SELECT k.id, m.link_id, m.source_id, sp.key, s.number, s.title, perm_project_holds(s.project_id, current_actor_id(), 'view'),
           m.implemented, m.verified, m.suspect, m.in_effect
    FROM unnest(targets) k(id)
    CROSS JOIN LATERAL risk_mitigations(k.id) m
    JOIN requirement s ON s.id = m.source_id
    JOIN project sp ON sp.id = s.project_id
    ORDER BY k.id, sp.key, s.number
$$;
-- +goose StatementEnd
