-- +goose Up
-- The query language (#47) names requirements by key and keeps them at a coverage level; both are read here, as the
-- reader sees them, so a query's SQL stays a WHERE clause over the requirement it narrows.

-- The requirement a key names: a key it left on a move first, then its project's key or one its project had, with its
-- number, as requirement.find reads one. A key nobody holds, or the reader may not see, names none.
-- +goose StatementBegin
CREATE FUNCTION requirement_key_id(project_key text, number integer) RETURNS uuid
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(
        (SELECT k.requirement_id FROM requirement_old_key k WHERE k.key = project_key || '-R' || requirement_key_id.number),
        (SELECT r.id FROM requirement r
         WHERE r.number = requirement_key_id.number AND r.project_id IN (
               SELECT x.id FROM project x WHERE x.key = project_key
               UNION ALL SELECT a.project_id FROM project_key_alias a WHERE a.key = project_key)))
$$;
-- +goose StatementEnd

-- How far a requirement is covered in one dimension, decomposed, implemented or verified (#38): na when its type's
-- rule, or the project's for every type, does not ask for it, and for a section. It judges the links the reader sees
-- as trace.Judge does from the counts of judgeLinks; an integration test holds the two to the same answers.
-- +goose StatementBegin
CREATE FUNCTION requirement_coverage(target uuid, dimension text) RETURNS text
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    WITH req AS (
        SELECT r.id, r.kind, r.revision,
               CASE dimension
                   WHEN 'decomposed' THEN COALESCE(own.decomposed, every.decomposed, false)
                   WHEN 'implemented' THEN COALESCE(own.implemented, every.implemented, false)
                   WHEN 'verified' THEN COALESCE(own.verified, every.verified, false)
               END AS asks
        FROM requirement r
        LEFT JOIN coverage_rule own ON own.project_id = r.project_id AND own.type_id = r.type_id
        LEFT JOIN coverage_rule every ON every.project_id = r.project_id AND every.type_id IS NULL
        WHERE r.id = target
    ), t AS (
        SELECT count(*) FILTER (WHERE lt.counts = 'decomposition') AS decomposing,
               count(*) FILTER (WHERE lt.counts = 'decomposition' AND v.suspect) AS decomposing_suspect,
               count(*) FILTER (WHERE lt.counts = 'verification') AS verifying,
               count(*) FILTER (WHERE lt.counts = 'verification' AND v.suspect) AS verifying_suspect
        FROM trace_link_state v
        JOIN link_type lt ON lt.id = v.type_id
        JOIN requirement s ON s.id = v.source_id
        -- What waits in the trash covers nothing.
        WHERE v.target_id = target AND s.trashed_at IS NULL AND lt.counts <> 'nothing'
    ), a AS (
        SELECT count(*) FILTER (WHERE l.relation = 'implements') AS implementing,
               count(*) FILTER (WHERE l.relation = 'implements' AND l.done) AS implementing_done,
               count(*) FILTER (WHERE l.relation = 'implements' AND l.suspect) AS implementing_suspect,
               count(*) FILTER (WHERE l.relation = 'verifies') AS verifying,
               count(*) FILTER (WHERE l.relation = 'verifies' AND l.done) AS verifying_done,
               count(*) FILTER (WHERE l.relation = 'verifies' AND l.suspect) AS verifying_suspect
        -- A category Armature has not told since the issue moved is not taken for done.
        FROM (SELECT al.relation, al.status_category = 'done' AND NOT al.status_stale AS done,
                     req.revision > al.reviewed_rev AS suspect
              FROM armature_issue_link al, req
              WHERE al.requirement_id = target AND al.broken_at IS NULL AND al.detached_at IS NULL) l
    )
    SELECT CASE
        WHEN req.kind <> 'requirement' OR NOT req.asks THEN 'na'
        WHEN dimension = 'decomposed' THEN
            CASE WHEN t.decomposing = 0 THEN 'none' WHEN t.decomposing_suspect = 0 THEN 'full' ELSE 'partial' END
        WHEN dimension = 'implemented' THEN
            CASE WHEN a.implementing = 0 THEN 'none'
                 WHEN a.implementing_done > 0 AND a.implementing_suspect = 0 THEN 'full' ELSE 'partial' END
        ELSE
            CASE WHEN t.verifying + a.verifying = 0 THEN 'none'
                 WHEN t.verifying_suspect + a.verifying_suspect = 0 AND a.verifying_done = a.verifying THEN 'full'
                 ELSE 'partial' END
    END
    FROM req, t, a
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS requirement_coverage(uuid, text);
DROP FUNCTION IF EXISTS requirement_key_id(text, integer);
