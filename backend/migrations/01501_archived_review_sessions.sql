-- +goose Up
-- An archived project takes no change, and the sessions it holds are part of it: as opening one already needs an
-- open project, so does changing, moving on or closing one (#351). The reminder writes as the admin role.
DROP POLICY review_session_moderators ON review_session;
CREATE POLICY review_session_moderators ON review_session AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (review_session_moderated(id, current_actor_id())
           AND EXISTS (SELECT 1 FROM project p WHERE p.id = project_id AND p.archived_at IS NULL))
    WITH CHECK (review_session_moderated(id, current_actor_id()));

-- +goose Down
DROP POLICY review_session_moderators ON review_session;
CREATE POLICY review_session_moderators ON review_session AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (review_session_moderated(id, current_actor_id()))
    WITH CHECK (review_session_moderated(id, current_actor_id()));
