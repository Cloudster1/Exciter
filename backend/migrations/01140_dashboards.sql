-- +goose Up
-- Dashboards (#212), as docs/api-contract-m11.md agrees: a dashboard belongs to a project and a person, theirs alone
-- until shared with whoever may view the project; its widgets are read live as the reader and keep nothing.

CREATE TABLE dashboard (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    owner_id   uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    -- dashboard.MaxNameLength.
    name       text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 80),
    shared     boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX dashboard_name_idx ON dashboard (project_id, owner_id, lower(name));
CREATE INDEX dashboard_project_idx ON dashboard (project_id);

CREATE TABLE dashboard_widget (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    dashboard_id uuid NOT NULL REFERENCES dashboard(id) ON DELETE CASCADE,
    kind         text NOT NULL CHECK (kind IN ('breakdown', 'count', 'list', 'coverage', 'review', 'suspects', 'changes', 'quality')),
    -- dashboard.MaxTitleLength.
    title        text NOT NULL CHECK (btrim(title) <> '' AND char_length(title) <= 80),
    position     integer NOT NULL CHECK (position >= 0),
    -- dashboard.MaxWidth.
    width        integer NOT NULL CHECK (width BETWEEN 1 AND 3),
    config       jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(config) = 'object'),
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dashboard_widget_dashboard_idx ON dashboard_widget (dashboard_id, position);

-- +goose StatementBegin
-- Whether the person asking may change a dashboard: its owner, or once it is shared the project's administrators.
CREATE FUNCTION dashboard_kept(dashboard uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM dashboard d WHERE d.id = dashboard_kept.dashboard
                   AND perm_project_holds(d.project_id, current_actor_id(), 'view')
                   AND (d.owner_id = current_actor_id() OR (d.shared AND perm_project_holds(d.project_id, current_actor_id(), 'administer'))))
$$;

-- Clears a project's home dashboard once it is gone or no longer shared, for whoever deleted or unshared it.
CREATE FUNCTION project_clear_home_dashboard(project uuid) RETURNS void
    LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    UPDATE project p SET settings = p.settings - 'homeDashboardId'
    WHERE p.id = project_clear_home_dashboard.project AND p.settings ? 'homeDashboardId'
      AND NOT EXISTS (SELECT 1 FROM dashboard d WHERE d.id::text = p.settings ->> 'homeDashboardId' AND d.shared)
$$;

-- A dashboard stays its owner's and its project's.
CREATE FUNCTION dashboard_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.project_id, NEW.owner_id) IS DISTINCT FROM (OLD.project_id, OLD.owner_id) THEN
        RAISE EXCEPTION 'a dashboard keeps its project and its owner'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'dashboard_fixed';
    END IF;
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER dashboard_fixed BEFORE UPDATE ON dashboard FOR EACH ROW EXECUTE FUNCTION dashboard_fixed();

ALTER TABLE dashboard ENABLE ROW LEVEL SECURITY;
ALTER TABLE dashboard FORCE  ROW LEVEL SECURITY;
CREATE POLICY dashboard_installation ON dashboard USING (true) WITH CHECK (true);
CREATE POLICY dashboard_readers ON dashboard AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view') AND (owner_id = current_actor_id() OR shared));
-- Sharing a dashboard needs edit on its project; making one, view.
CREATE POLICY dashboard_makers ON dashboard AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (owner_id = current_actor_id() AND perm_project_holds(project_id, current_actor_id(), 'view')
                AND (NOT shared OR perm_project_holds(project_id, current_actor_id(), 'edit')));
CREATE POLICY dashboard_changers ON dashboard AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (dashboard_kept(id))
    WITH CHECK (NOT shared OR perm_project_holds(project_id, current_actor_id(), 'edit'));
CREATE POLICY dashboard_removers ON dashboard AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (dashboard_kept(id));

ALTER TABLE dashboard_widget ENABLE ROW LEVEL SECURITY;
ALTER TABLE dashboard_widget FORCE  ROW LEVEL SECURITY;
CREATE POLICY dashboard_widget_installation ON dashboard_widget USING (true) WITH CHECK (true);
CREATE POLICY dashboard_widget_readers ON dashboard_widget AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM dashboard d WHERE d.id = dashboard_id));
CREATE POLICY dashboard_widget_makers ON dashboard_widget AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (dashboard_kept(dashboard_id));
CREATE POLICY dashboard_widget_changers ON dashboard_widget AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (dashboard_kept(dashboard_id)) WITH CHECK (dashboard_kept(dashboard_id));
CREATE POLICY dashboard_widget_removers ON dashboard_widget AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (dashboard_kept(dashboard_id));

GRANT SELECT, INSERT, UPDATE, DELETE ON dashboard, dashboard_widget TO exciter_admin;
REVOKE ALL ON dashboard, dashboard_widget FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON dashboard, dashboard_widget TO exciter_app;
GRANT UPDATE (name, shared) ON dashboard TO exciter_app;
GRANT UPDATE (title, width, position, config) ON dashboard_widget TO exciter_app;
REVOKE ALL ON FUNCTION dashboard_kept(uuid), project_clear_home_dashboard(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION dashboard_kept(uuid), project_clear_home_dashboard(uuid) TO exciter_app, exciter_admin;

-- +goose Down
UPDATE project SET settings = settings - 'homeDashboardId' WHERE settings ? 'homeDashboardId';
DROP TABLE dashboard_widget;
DROP TABLE dashboard;
DROP FUNCTION dashboard_kept(uuid);
DROP FUNCTION project_clear_home_dashboard(uuid);
DROP FUNCTION dashboard_fixed();
