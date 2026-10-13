-- +goose Up
-- A saved view is a grid of a project kept under a name, shaped like Armature's saved filters: its owner's, and
-- with shared offered to everybody who may view the project. The filter, columns and order are the grid's own
-- words, which the service checks before it saves them.
CREATE TABLE saved_view (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL,
    project_id uuid NOT NULL,
    owner_id   uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100 AND name = btrim(name)),
    shared     boolean NOT NULL DEFAULT false,
    filter     jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(filter) = 'object'),
    columns    text[] NOT NULL DEFAULT '{}' CHECK (cardinality(columns) <= 30),
    sort       jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(sort) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE
);

-- One name per owner and project, whatever its case.
CREATE UNIQUE INDEX saved_view_name_key ON saved_view (org_id, project_id, owner_id, lower(name));

CREATE TRIGGER saved_view_set_updated_at BEFORE UPDATE ON saved_view
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE saved_view ENABLE ROW LEVEL SECURITY;
ALTER TABLE saved_view FORCE  ROW LEVEL SECURITY;

CREATE POLICY saved_view_tenant_isolation ON saved_view
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY saved_view_admin_bypass ON saved_view TO exciter_admin USING (true) WITH CHECK (true);

-- A view is read by its owner, and once shared by everybody who may view its project. It is saved in one's own
-- name, shared only by who edits there, and changed or deleted by its owner, or once shared by the project's
-- administrators.
CREATE POLICY saved_view_visible ON saved_view AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view') AND (owner_id = current_actor_id() OR shared));
CREATE POLICY saved_view_added ON saved_view AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (owner_id = current_actor_id() AND perm_project_holds(project_id, current_actor_id(), 'view')
                AND (NOT shared OR perm_project_holds(project_id, current_actor_id(), 'edit')));
CREATE POLICY saved_view_changed ON saved_view AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (owner_id = current_actor_id() OR (shared AND perm_project_holds(project_id, current_actor_id(), 'administer')))
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'view')
                AND (owner_id = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'))
                AND (NOT shared OR perm_project_holds(project_id, current_actor_id(), 'edit')));
CREATE POLICY saved_view_removed ON saved_view AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (owner_id = current_actor_id() OR (shared AND perm_project_holds(project_id, current_actor_id(), 'administer')));

-- A view keeps its owner and project: the app role changes only what a view says.
GRANT SELECT, INSERT, UPDATE, DELETE ON saved_view TO exciter_admin;
REVOKE ALL ON saved_view FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON saved_view TO exciter_app;
GRANT UPDATE (name, shared, filter, columns, sort) ON saved_view TO exciter_app;

-- +goose Down
DROP TABLE IF EXISTS saved_view;
