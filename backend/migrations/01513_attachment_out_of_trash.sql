-- +goose Up
-- A file goes only on a requirement out of the trash (#312). The service refused one there, but the policy asked
-- only for edit, which an editor holds in the trash too, so a writer straight through SQL attached one.
DROP POLICY attachment_uploaded_by_editors ON attachment;
CREATE POLICY attachment_uploaded_by_editors ON attachment AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (uploaded_by = current_actor_id() AND deleted_at IS NULL AND deleted_by IS NULL
                AND perm_requirement_editable(requirement_id, current_actor_id())
                AND EXISTS (SELECT 1 FROM requirement r WHERE r.id = requirement_id AND r.trashed_at IS NULL));

-- +goose Down
DROP POLICY attachment_uploaded_by_editors ON attachment;
CREATE POLICY attachment_uploaded_by_editors ON attachment AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (uploaded_by = current_actor_id() AND deleted_at IS NULL AND deleted_by IS NULL
                AND perm_requirement_editable(requirement_id, current_actor_id()));
