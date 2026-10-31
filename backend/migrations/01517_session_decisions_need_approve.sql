-- +goose Up
-- A session's approvers are named by whoever opens it, who needs only edit, and a group named there may hold people
-- who never were given the approve right; so a decision on a session's review asks for that right when it is made,
-- whoever the session names. A review its type's rule asks keeps the approvers a project administrator named in the
-- rule, which is the administrator granting them that one decision. A rejection closes a review as surely as the
-- last approval does, so it asks the same right.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION review_decidable(review uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM requirement_review v
        JOIN requirement r ON r.id = v.requirement_id
        JOIN requirement_revision rv ON rv.requirement_id = v.requirement_id AND rv.number = v.revision
        WHERE v.id = review AND v.state = 'pending' AND r.revision = v.revision
          AND (v.author_may_approve OR rv.created_by IS DISTINCT FROM actor)
          AND perm_requirement_viewable(v.requirement_id, actor) AND review_names(review, actor)
          AND (v.session_id IS NULL OR (perm_project_holds(r.project_id, actor, 'approve')
               AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = v.session_id AND s.state = 'open'))))
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION review_decidable(review uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM requirement_review v
        JOIN requirement r ON r.id = v.requirement_id
        JOIN requirement_revision rv ON rv.requirement_id = v.requirement_id AND rv.number = v.revision
        WHERE v.id = review AND v.state = 'pending' AND r.revision = v.revision
          AND (v.author_may_approve OR rv.created_by IS DISTINCT FROM actor)
          AND perm_requirement_viewable(v.requirement_id, actor) AND review_names(review, actor)
          AND (v.session_id IS NULL OR EXISTS (SELECT 1 FROM review_session s WHERE s.id = v.session_id AND s.state = 'open')))
$$;
-- +goose StatementEnd
