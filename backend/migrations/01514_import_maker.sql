-- +goose Up
-- An import is its maker's alone to map and start (#318). The service refused anybody else, but the policy let every
-- administrator of the project change the row, so another administrator mapped and started it straight through SQL.
-- They still read it and stop it, which is the one change they make.
DROP POLICY import_job_changed ON import_job;
CREATE POLICY import_job_changed ON import_job AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'))
    WITH CHECK (created_by = current_actor_id()
                OR (state = 'cancelled' AND perm_project_holds(project_id, current_actor_id(), 'administer')));

DROP POLICY reqif_import_changed ON reqif_import;
CREATE POLICY reqif_import_changed ON reqif_import AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'))
    WITH CHECK (created_by = current_actor_id()
                OR (state = 'cancelled' AND perm_project_holds(project_id, current_actor_id(), 'administer')));

-- +goose Down
DROP POLICY reqif_import_changed ON reqif_import;
CREATE POLICY reqif_import_changed ON reqif_import AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'))
    WITH CHECK (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'));

DROP POLICY import_job_changed ON import_job;
CREATE POLICY import_job_changed ON import_job AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'))
    WITH CHECK (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'));
