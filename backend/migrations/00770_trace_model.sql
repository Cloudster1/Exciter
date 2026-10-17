-- +goose Up
-- A project's trace model (#151): its levels in order, such as stakeholder, system, test and Armature issue, the
-- requirement types each holds and the links that lead to it from the level before, as docs/api-contract-m5.md
-- agrees. The chains themselves are read from the links, never stored. A model stays inside its project.
CREATE TABLE trace_level (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    position   smallint NOT NULL CHECK (position BETWEEN 0 AND 5),
    name       text NOT NULL CHECK (name <> '' AND char_length(name) <= 40),
    kind       text NOT NULL CHECK (kind IN ('requirements', 'issues', 'pages')),
    updated_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT trace_level_place UNIQUE (project_id, position),
    -- The children carry the project too, so their policies read it without a join, and it is always the level's.
    CONSTRAINT trace_level_of_project UNIQUE (id, project_id)
);
CREATE UNIQUE INDEX trace_level_name_idx ON trace_level (project_id, lower(name));

-- The requirement types a requirements level holds.
CREATE TABLE trace_level_type (
    level_id   uuid NOT NULL,
    project_id uuid NOT NULL,
    type_id    uuid NOT NULL REFERENCES requirement_type(id) ON DELETE CASCADE,
    PRIMARY KEY (level_id, type_id),
    FOREIGN KEY (level_id, project_id) REFERENCES trace_level(id, project_id) ON DELETE CASCADE
);

-- One way down to a level from the one before: a link type read in a direction, from the upper level, or an
-- Armature or Stator relation for an issues or pages level.
CREATE TABLE trace_level_step (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    level_id     uuid NOT NULL,
    project_id   uuid NOT NULL,
    link_type_id uuid REFERENCES link_type(id) ON DELETE CASCADE,
    direction    text CHECK (direction IN ('outgoing', 'incoming')),
    relation     text CHECK (relation IN ('implements', 'verifies', 'relates', 'specifies', 'describes')),
    FOREIGN KEY (level_id, project_id) REFERENCES trace_level(id, project_id) ON DELETE CASCADE,
    CONSTRAINT trace_level_step_one_way CHECK (
        (link_type_id IS NOT NULL AND direction IS NOT NULL AND relation IS NULL)
        OR (link_type_id IS NULL AND direction IS NULL AND relation IS NOT NULL))
);
CREATE UNIQUE INDEX trace_level_step_once ON trace_level_step (level_id, link_type_id, direction, relation) NULLS NOT DISTINCT;

-- +goose StatementBegin
-- Whether the person administers the project and it is not archived: an archived project's model stands as it was.
CREATE FUNCTION trace_model_keeper(target_project uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_project_holds(target_project, actor, 'administer')
           AND EXISTS (SELECT 1 FROM project p WHERE p.id = target_project AND p.archived_at IS NULL);
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- A level's type is one of its own project's, so a model never reaches into another project.
CREATE FUNCTION trace_level_type_held(target_project uuid, target_type uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM requirement_type t WHERE t.id = target_type AND t.project_id = target_project);
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- A step's link type is the installation's or the project's own, never another project's.
CREATE FUNCTION trace_level_link_type_held(target_project uuid, target_type uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT target_type IS NULL
           OR EXISTS (SELECT 1 FROM link_type t WHERE t.id = target_type AND (t.project_id IS NULL OR t.project_id = target_project));
$$;
-- +goose StatementEnd

ALTER TABLE trace_level ENABLE ROW LEVEL SECURITY;
ALTER TABLE trace_level FORCE  ROW LEVEL SECURITY;
ALTER TABLE trace_level_type ENABLE ROW LEVEL SECURITY;
ALTER TABLE trace_level_type FORCE  ROW LEVEL SECURITY;
ALTER TABLE trace_level_step ENABLE ROW LEVEL SECURITY;
ALTER TABLE trace_level_step FORCE  ROW LEVEL SECURITY;

-- One open policy per table, as every table has since #106, and the permission policies beside it.
CREATE POLICY trace_level_installation ON trace_level USING (true) WITH CHECK (true);
CREATE POLICY trace_level_type_installation ON trace_level_type USING (true) WITH CHECK (true);
CREATE POLICY trace_level_step_installation ON trace_level_step USING (true) WITH CHECK (true);

-- A project's model is read by whoever views it and set by whoever administers it.
CREATE POLICY trace_level_readers ON trace_level AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY trace_level_setters ON trace_level AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (trace_model_keeper(project_id, current_actor_id()));
CREATE POLICY trace_level_removers ON trace_level AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (trace_model_keeper(project_id, current_actor_id()));

CREATE POLICY trace_level_type_readers ON trace_level_type AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY trace_level_type_setters ON trace_level_type AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (trace_model_keeper(project_id, current_actor_id()) AND trace_level_type_held(project_id, type_id));
CREATE POLICY trace_level_type_removers ON trace_level_type AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (trace_model_keeper(project_id, current_actor_id()));

CREATE POLICY trace_level_step_readers ON trace_level_step AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY trace_level_step_setters ON trace_level_step AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (trace_model_keeper(project_id, current_actor_id()) AND trace_level_link_type_held(project_id, link_type_id));
CREATE POLICY trace_level_step_removers ON trace_level_step AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (trace_model_keeper(project_id, current_actor_id()));

-- A model is replaced whole, never changed in place, so the app role inserts and deletes and updates nothing.
GRANT SELECT, INSERT, UPDATE, DELETE ON trace_level, trace_level_type, trace_level_step TO exciter_admin;
REVOKE ALL ON trace_level, trace_level_type, trace_level_step FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON trace_level, trace_level_type, trace_level_step TO exciter_app;
REVOKE ALL ON FUNCTION trace_model_keeper(uuid, uuid), trace_level_type_held(uuid, uuid), trace_level_link_type_held(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION trace_model_keeper(uuid, uuid), trace_level_type_held(uuid, uuid), trace_level_link_type_held(uuid, uuid)
    TO exciter_app, exciter_admin;

-- +goose Down
DROP TABLE trace_level_step;
DROP TABLE trace_level_type;
DROP TABLE trace_level;
DROP FUNCTION trace_level_link_type_held(uuid, uuid);
DROP FUNCTION trace_level_type_held(uuid, uuid);
DROP FUNCTION trace_model_keeper(uuid, uuid);
