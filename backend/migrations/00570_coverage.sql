-- +goose Up
-- Coverage rules (#38): which of decomposed, implemented and verified a project asks of its requirements of one
-- type, or of every type without its own, as approval rules do. Without a rule nothing is asked. The levels
-- themselves are read from the links, never stored, so no change of a link has a copy to keep up with.
CREATE TABLE coverage_rule (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id  uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    type_id     uuid REFERENCES requirement_type(id) ON DELETE CASCADE,
    decomposed  boolean NOT NULL DEFAULT false,
    implemented boolean NOT NULL DEFAULT false,
    verified    boolean NOT NULL DEFAULT false,
    updated_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
-- One rule per type, and one for every type without its own.
CREATE UNIQUE INDEX coverage_rule_type_idx ON coverage_rule (project_id, type_id) NULLS NOT DISTINCT;

-- +goose StatementBegin
-- Whether the person administers the rule's project and it is not archived: an archived project's rules stand
-- as they were, with the requirements they judge.
CREATE FUNCTION coverage_rule_keeper(target_project uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_project_holds(target_project, actor, 'administer')
           AND EXISTS (SELECT 1 FROM project p WHERE p.id = target_project AND p.archived_at IS NULL);
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- A rule's type is one of its own project's, so no rule judges another project's requirements.
CREATE FUNCTION coverage_rule_type_held(target_project uuid, target_type uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT target_type IS NULL
           OR EXISTS (SELECT 1 FROM requirement_type t WHERE t.id = target_type AND t.project_id = target_project);
$$;
-- +goose StatementEnd

ALTER TABLE coverage_rule ENABLE ROW LEVEL SECURITY;
ALTER TABLE coverage_rule FORCE  ROW LEVEL SECURITY;

-- One open policy per table, as every table has since #106, and the permission policies beside it.
CREATE POLICY coverage_rule_installation ON coverage_rule USING (true) WITH CHECK (true);

-- A project's rules are read by whoever views it and set by whoever administers it.
CREATE POLICY coverage_rule_readers ON coverage_rule AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY coverage_rule_setters ON coverage_rule AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (coverage_rule_keeper(project_id, current_actor_id()) AND coverage_rule_type_held(project_id, type_id));
CREATE POLICY coverage_rule_changers ON coverage_rule AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (coverage_rule_keeper(project_id, current_actor_id()))
    WITH CHECK (coverage_rule_keeper(project_id, current_actor_id()) AND coverage_rule_type_held(project_id, type_id));
CREATE POLICY coverage_rule_removers ON coverage_rule AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (coverage_rule_keeper(project_id, current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON coverage_rule TO exciter_admin;
REVOKE ALL ON coverage_rule FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON coverage_rule TO exciter_app;
GRANT UPDATE (decomposed, implemented, verified, updated_by, updated_at) ON coverage_rule TO exciter_app;
REVOKE ALL ON FUNCTION coverage_rule_keeper(uuid, uuid), coverage_rule_type_held(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION coverage_rule_keeper(uuid, uuid), coverage_rule_type_held(uuid, uuid) TO exciter_app, exciter_admin;

-- +goose Down
DROP TABLE coverage_rule;
DROP FUNCTION coverage_rule_type_held(uuid, uuid);
DROP FUNCTION coverage_rule_keeper(uuid, uuid);
