-- +goose Up
-- Statuses and workflows (#17): a slimmed Armature workflow, per project. A project follows one workflow and a
-- type may follow its own. A requirement's status changes only through the functions below, which hold it to
-- the transitions of its workflow; the app role has no UPDATE on the column.
CREATE TABLE req_status (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id  uuid NOT NULL,
    name        text NOT NULL,
    category    text NOT NULL CHECK (category IN ('draft', 'review', 'approved', 'done', 'closed')),
    description text NOT NULL DEFAULT '',
    position    integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT req_status_org_id_key UNIQUE (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    CONSTRAINT req_status_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 60)
);

CREATE UNIQUE INDEX req_status_name_idx ON req_status (project_id, lower(name));

CREATE TABLE workflow (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id  uuid NOT NULL,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workflow_org_id_key UNIQUE (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    CONSTRAINT workflow_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 60)
);

CREATE UNIQUE INDEX workflow_name_idx ON workflow (project_id, lower(name));

-- A status's place in one workflow: whether requirements start there, where a new revision sends one in it,
-- and where the designer drew it.
CREATE TABLE workflow_step (
    org_id                uuid NOT NULL,
    workflow_id           uuid NOT NULL,
    status_id             uuid NOT NULL,
    is_initial            boolean NOT NULL DEFAULT false,
    on_revision_status_id uuid,
    layout_x              integer,
    layout_y              integer,
    PRIMARY KEY (workflow_id, status_id),
    FOREIGN KEY (org_id, workflow_id) REFERENCES workflow (org_id, id) ON DELETE CASCADE,
    -- Checked at commit, so a project's cascade may take its statuses before the steps that name them.
    FOREIGN KEY (org_id, status_id) REFERENCES req_status (org_id, id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY (org_id, on_revision_status_id) REFERENCES req_status (org_id, id) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT workflow_step_layout_whole CHECK ((layout_x IS NULL) = (layout_y IS NULL))
);

CREATE UNIQUE INDEX workflow_step_one_initial ON workflow_step (workflow_id) WHERE is_initial;

-- A null from is a transition offered from every status.
CREATE TABLE workflow_transition (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id         uuid NOT NULL,
    workflow_id    uuid NOT NULL,
    name           text NOT NULL,
    description    text NOT NULL DEFAULT '',
    from_status_id uuid,
    to_status_id   uuid NOT NULL,
    position       integer NOT NULL DEFAULT 0,
    CONSTRAINT workflow_transition_org_id_key UNIQUE (org_id, id),
    FOREIGN KEY (org_id, workflow_id) REFERENCES workflow (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workflow_id, to_status_id) REFERENCES workflow_step (workflow_id, status_id) ON DELETE CASCADE,
    FOREIGN KEY (workflow_id, from_status_id) REFERENCES workflow_step (workflow_id, status_id) ON DELETE CASCADE,
    CONSTRAINT workflow_transition_moves CHECK (from_status_id IS DISTINCT FROM to_status_id),
    CONSTRAINT workflow_transition_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 60)
);

CREATE TABLE transition_rule (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL,
    transition_id uuid NOT NULL,
    kind          text NOT NULL CHECK (kind IN ('condition', 'validator', 'postfunction')),
    type          text NOT NULL,
    config        jsonb NOT NULL DEFAULT '{}'::jsonb,
    position      integer NOT NULL DEFAULT 0,
    FOREIGN KEY (org_id, transition_id) REFERENCES workflow_transition (org_id, id) ON DELETE CASCADE
);

CREATE INDEX transition_rule_transition_idx ON transition_rule (transition_id, position);

ALTER TABLE requirement_type
    ADD COLUMN workflow_id uuid,
    ADD CONSTRAINT requirement_type_workflow_fkey FOREIGN KEY (org_id, workflow_id) REFERENCES workflow (org_id, id)
        DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE requirement
    ADD COLUMN status_id uuid,
    ADD CONSTRAINT requirement_status_fkey FOREIGN KEY (org_id, status_id) REFERENCES req_status (org_id, id)
        DEFERRABLE INITIALLY DEFERRED,
    ADD CONSTRAINT requirement_section_statusless CHECK (kind = 'requirement' OR status_id IS NULL);

CREATE INDEX requirement_status_idx ON requirement (org_id, status_id) WHERE status_id IS NOT NULL;

CREATE TRIGGER req_status_set_updated_at BEFORE UPDATE ON req_status FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER workflow_set_updated_at BEFORE UPDATE ON workflow FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The workflow a requirement follows: its type's own, else its project's.
-- +goose StatementBegin
CREATE FUNCTION requirement_workflow(type_id uuid, project_id uuid) RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(
        (SELECT t.workflow_id FROM requirement_type t WHERE t.id = requirement_workflow.type_id),
        (SELECT (p.settings ->> 'workflowId')::uuid FROM project p WHERE p.id = requirement_workflow.project_id))
$$;
-- +goose StatementEnd

-- A new requirement starts in its workflow's initial status, whatever an insert says, and a section has none.
-- +goose StatementBegin
CREATE FUNCTION requirement_status_initial() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.kind = 'section' THEN
        NEW.status_id := NULL;
        RETURN NEW;
    END IF;
    SELECT s.status_id INTO NEW.status_id FROM workflow_step s
    WHERE s.workflow_id = requirement_workflow(NEW.type_id, NEW.project_id) AND s.is_initial;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_status_initial BEFORE INSERT ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_status_initial();

-- The project roles a person holds, as the grants that reach them give them: what a role condition asks.
-- +goose StatementBegin
CREATE FUNCTION perm_project_roles(target uuid, actor uuid) RETURNS text[]
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(array_agg(DISTINCT role), '{}') FROM (
        SELECT 'admin' AS role FROM org_member m JOIN project p ON p.org_id = m.org_id
        WHERE p.id = target AND m.user_id = actor AND m.org_role IN ('owner', 'admin')
        UNION ALL
        SELECT perm_grant_role(m.org_role, g.role) FROM project_grant g
        JOIN org_member m ON m.org_id = g.org_id AND m.user_id = actor
        WHERE g.project_id = target AND perm_grant_reaches(actor, m.org_role, g.subject_type, g.user_id, g.group_id)
    ) roles
$$;
-- +goose StatementEnd

-- Takes a transition: one of the requirement's workflow that leaves its status, or is offered from every
-- status, and is the actor's to take, by edit, or by approve where a role condition names reviewers. The
-- service checks the rest of the conditions and the validators first. The status change and the owner its
-- post-functions set are logged here, since an approver holds no edit to log or set them through the app role.
-- +goose StatementBegin
CREATE FUNCTION requirement_transition(requirement uuid, transition uuid, note text DEFAULT '') RETURNS uuid
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    r requirement%ROWTYPE;
    t workflow_transition%ROWTYPE;
    actor uuid := current_actor_id();
    new_owner uuid;
    post text;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_transition.requirement AND org_id = current_org_id() FOR UPDATE;
    SELECT * INTO t FROM workflow_transition WHERE id = requirement_transition.transition AND org_id = current_org_id();
    IF r.id IS NULL OR t.id IS NULL OR r.trashed_at IS NOT NULL OR t.workflow_id <> requirement_workflow(r.type_id, r.project_id) THEN
        RAISE EXCEPTION 'that transition is not one of the requirement''s workflow'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_transition_known';
    END IF;
    IF NOT (perm_project_holds(r.project_id, actor, 'edit') OR (perm_project_holds(r.project_id, actor, 'approve') AND EXISTS (
        SELECT 1 FROM transition_rule x WHERE x.transition_id = t.id AND x.type = 'condition.project_role' AND x.config -> 'roles' ? 'reviewer'))) THEN
        RAISE EXCEPTION 'the transition is not the actor''s to take'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'requirement_transition_allowed';
    END IF;
    IF (t.from_status_id IS NOT NULL AND t.from_status_id IS DISTINCT FROM r.status_id)
       OR (t.from_status_id IS NULL AND t.to_status_id = r.status_id) THEN
        RAISE EXCEPTION 'the transition does not leave the requirement''s status'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_transition_leaves';
    END IF;
    new_owner := r.owner_id;
    FOR post IN SELECT x.type FROM transition_rule x WHERE x.transition_id = t.id AND x.kind = 'postfunction' ORDER BY x.position LOOP
        new_owner := CASE post WHEN 'postfunction.assign_to_actor' THEN actor WHEN 'postfunction.clear_owner' THEN NULL ELSE new_owner END;
    END LOOP;
    UPDATE requirement SET status_id = t.to_status_id, owner_id = new_owner, updated_by = actor WHERE id = r.id;
    INSERT INTO requirement_change (org_id, requirement_id, field, from_value, to_value, reason, actor_id)
    VALUES (r.org_id, r.id, 'status', COALESCE((SELECT name FROM req_status WHERE id = r.status_id), ''),
            (SELECT name FROM req_status WHERE id = t.to_status_id), COALESCE(note, ''), actor);
    IF new_owner IS DISTINCT FROM r.owner_id THEN
        INSERT INTO requirement_change (org_id, requirement_id, field, from_value, to_value, actor_id)
        VALUES (r.org_id, r.id, 'owner', COALESCE((SELECT name FROM app_user WHERE id = r.owner_id), ''),
                COALESCE((SELECT name FROM app_user WHERE id = new_owner), ''), actor);
    END IF;
    RETURN t.to_status_id;
END;
$$;
-- +goose StatementEnd

-- Moves a requirement whose status its workflow lacks, after a move, a new type or a new workflow, to one the
-- workflow has; a status the workflow has is no one's to change this way.
-- +goose StatementBegin
CREATE FUNCTION requirement_remap_status(requirement uuid, status uuid) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    r requirement%ROWTYPE;
    flow uuid;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_remap_status.requirement AND org_id = current_org_id() FOR UPDATE;
    IF r.id IS NULL OR r.kind <> 'requirement' OR NOT perm_project_holds(r.project_id, current_actor_id(), 'edit') THEN
        RAISE EXCEPTION 'the requirement is not the actor''s to change'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'requirement_remap_allowed';
    END IF;
    flow := requirement_workflow(r.type_id, r.project_id);
    IF EXISTS (SELECT 1 FROM workflow_step WHERE workflow_id = flow AND status_id = r.status_id)
       OR NOT EXISTS (SELECT 1 FROM workflow_step WHERE workflow_id = flow AND status_id = requirement_remap_status.status) THEN
        RAISE EXCEPTION 'a status is remapped only from one the workflow lacks to one it has'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_remap_lacking';
    END IF;
    UPDATE requirement SET status_id = requirement_remap_status.status WHERE id = r.id;
END;
$$;
-- +goose StatementEnd

-- Sends a requirement where its step says a new revision goes, as an approval a revision supersedes; answers
-- the status it left, null when it stayed.
-- +goose StatementBegin
CREATE FUNCTION requirement_revision_status(requirement uuid) RETURNS uuid
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    r requirement%ROWTYPE;
    target uuid;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_revision_status.requirement AND org_id = current_org_id() FOR UPDATE;
    IF r.id IS NULL OR NOT perm_project_holds(r.project_id, current_actor_id(), 'edit') THEN
        RETURN NULL;
    END IF;
    SELECT on_revision_status_id INTO target FROM workflow_step
    WHERE workflow_id = requirement_workflow(r.type_id, r.project_id) AND status_id = r.status_id;
    IF target IS NULL OR target = r.status_id THEN
        RETURN NULL;
    END IF;
    UPDATE requirement SET status_id = target WHERE id = r.id;
    RETURN r.status_id;
END;
$$;
-- +goose StatementEnd

-- Every project starts with the default workflow: Draft to Verified, with Rejected and Obsolete beside.
-- +goose StatementBegin
CREATE FUNCTION project_workflow_default(target uuid) RETURNS uuid
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    org uuid;
    flow uuid;
    draft uuid; review uuid; approved uuid; implemented uuid; verified uuid; rejected uuid; obsolete uuid;
    editors jsonb := '{"roles": ["editor", "admin"]}';
    reviewers jsonb := '{"roles": ["reviewer", "admin"]}';
BEGIN
    SELECT org_id INTO org FROM project WHERE id = target;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Draft', 'draft', 0) RETURNING id INTO draft;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'In review', 'review', 1) RETURNING id INTO review;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Approved', 'approved', 2) RETURNING id INTO approved;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Implemented', 'done', 3) RETURNING id INTO implemented;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Verified', 'done', 4) RETURNING id INTO verified;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Rejected', 'closed', 5) RETURNING id INTO rejected;
    INSERT INTO req_status (org_id, project_id, name, category, position) VALUES (org, target, 'Obsolete', 'closed', 6) RETURNING id INTO obsolete;
    INSERT INTO workflow (org_id, project_id, name, description)
    VALUES (org, target, 'Default workflow', 'Draft, review, approval, implementation and verification.') RETURNING id INTO flow;
    INSERT INTO workflow_step (org_id, workflow_id, status_id, is_initial, on_revision_status_id, layout_x, layout_y) VALUES
        (org, flow, draft, true, NULL, 40, 120), (org, flow, review, false, NULL, 280, 120), (org, flow, approved, false, draft, 520, 120),
        (org, flow, implemented, false, draft, 760, 120), (org, flow, verified, false, draft, 1000, 120),
        (org, flow, rejected, false, NULL, 400, 320), (org, flow, obsolete, false, NULL, 640, 320);
    PERFORM workflow_default_edge(org, flow, 'Submit for review', draft, review, 0, editors, false, false);
    PERFORM workflow_default_edge(org, flow, 'Approve', review, approved, 1, reviewers, false, true);
    PERFORM workflow_default_edge(org, flow, 'Send back', review, draft, 2, reviewers, true, false);
    PERFORM workflow_default_edge(org, flow, 'Mark implemented', approved, implemented, 3, editors, false, false);
    PERFORM workflow_default_edge(org, flow, 'Mark verified', implemented, verified, 4, editors, false, false);
    PERFORM workflow_default_edge(org, flow, 'Reject', NULL, rejected, 5, reviewers, true, false);
    PERFORM workflow_default_edge(org, flow, 'Make obsolete', NULL, obsolete, 6, editors, false, false);
    PERFORM workflow_default_edge(org, flow, 'Reopen', rejected, draft, 7, editors, false, false);
    PERFORM workflow_default_edge(org, flow, 'Reopen', obsolete, draft, 8, editors, false, false);
    UPDATE project SET settings = jsonb_set(settings, '{workflowId}', to_jsonb(flow::text)) WHERE id = target;
    RETURN flow;
END;
$$;
-- +goose StatementEnd

-- One transition of the default workflow with its role condition, and a comment or another author where asked.
-- +goose StatementBegin
CREATE FUNCTION workflow_default_edge(org uuid, flow uuid, edge_name text, from_status uuid, to_status uuid, place integer,
                                      roles jsonb, needs_comment boolean, not_author boolean) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    edge uuid;
BEGIN
    INSERT INTO workflow_transition (org_id, workflow_id, name, from_status_id, to_status_id, position)
    VALUES (org, flow, edge_name, from_status, to_status, place) RETURNING id INTO edge;
    INSERT INTO transition_rule (org_id, transition_id, kind, type, config, position) VALUES (org, edge, 'condition', 'condition.project_role', roles, 0);
    IF not_author THEN
        INSERT INTO transition_rule (org_id, transition_id, kind, type, position) VALUES (org, edge, 'condition', 'condition.not_author', 1);
    END IF;
    IF needs_comment THEN
        INSERT INTO transition_rule (org_id, transition_id, kind, type, position) VALUES (org, edge, 'validator', 'validator.comment_required', 2);
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION project_workflow_seed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM project_workflow_default(NEW.id);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_workflow_seed AFTER INSERT ON project
    FOR EACH ROW EXECUTE FUNCTION project_workflow_seed();

-- Projects made before workflows get the default one, and their requirements start in Draft.
SELECT project_workflow_default(id) FROM project;
UPDATE requirement r SET status_id = s.status_id
FROM workflow_step s
WHERE r.kind = 'requirement' AND s.workflow_id = requirement_workflow(r.type_id, r.project_id) AND s.is_initial;

ALTER TABLE req_status          ENABLE ROW LEVEL SECURITY;
ALTER TABLE req_status          FORCE  ROW LEVEL SECURITY;
ALTER TABLE workflow            ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow            FORCE  ROW LEVEL SECURITY;
ALTER TABLE workflow_step       ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_step       FORCE  ROW LEVEL SECURITY;
ALTER TABLE workflow_transition ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_transition FORCE  ROW LEVEL SECURITY;
ALTER TABLE transition_rule     ENABLE ROW LEVEL SECURITY;
ALTER TABLE transition_rule     FORCE  ROW LEVEL SECURITY;

-- The project a workflow belongs to, for the policies of the tables below it.
-- +goose StatementBegin
CREATE FUNCTION workflow_project(flow uuid) RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT project_id FROM workflow WHERE id = flow AND org_id = current_org_id()
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION transition_project(edge uuid) RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT w.project_id FROM workflow_transition t JOIN workflow w ON w.id = t.workflow_id WHERE t.id = edge AND t.org_id = current_org_id()
$$;
-- +goose StatementEnd

CREATE POLICY req_status_tenant_isolation ON req_status USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY workflow_tenant_isolation ON workflow USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY workflow_step_tenant_isolation ON workflow_step USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY workflow_transition_tenant_isolation ON workflow_transition USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY transition_rule_tenant_isolation ON transition_rule USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY req_status_admin_bypass ON req_status TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY workflow_admin_bypass ON workflow TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY workflow_step_admin_bypass ON workflow_step TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY workflow_transition_admin_bypass ON workflow_transition TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY transition_rule_admin_bypass ON transition_rule TO exciter_admin USING (true) WITH CHECK (true);

-- Through the app role whoever views a project reads its statuses and workflows, and its administrators shape
-- them while it is not archived.
CREATE POLICY req_status_visible ON req_status AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY req_status_added ON req_status AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY req_status_changed ON req_status AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id())) WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY req_status_removed ON req_status AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY workflow_visible ON workflow AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY workflow_added ON workflow AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY workflow_changed ON workflow AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id())) WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY workflow_removed ON workflow AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY workflow_step_visible ON workflow_step AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(workflow_project(workflow_id), current_actor_id(), 'view'));
CREATE POLICY workflow_step_added ON workflow_step AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_shaped(workflow_project(workflow_id), current_actor_id()));
CREATE POLICY workflow_step_changed ON workflow_step AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_shaped(workflow_project(workflow_id), current_actor_id())) WITH CHECK (perm_project_shaped(workflow_project(workflow_id), current_actor_id()));
CREATE POLICY workflow_step_removed ON workflow_step AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_shaped(workflow_project(workflow_id), current_actor_id()));
CREATE POLICY workflow_transition_visible ON workflow_transition AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(workflow_project(workflow_id), current_actor_id(), 'view'));
CREATE POLICY workflow_transition_added ON workflow_transition AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_shaped(workflow_project(workflow_id), current_actor_id()));
CREATE POLICY workflow_transition_changed ON workflow_transition AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_shaped(workflow_project(workflow_id), current_actor_id())) WITH CHECK (perm_project_shaped(workflow_project(workflow_id), current_actor_id()));
CREATE POLICY workflow_transition_removed ON workflow_transition AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_shaped(workflow_project(workflow_id), current_actor_id()));
CREATE POLICY transition_rule_visible ON transition_rule AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(transition_project(transition_id), current_actor_id(), 'view'));
CREATE POLICY transition_rule_added ON transition_rule AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_shaped(transition_project(transition_id), current_actor_id()));
CREATE POLICY transition_rule_changed ON transition_rule AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_shaped(transition_project(transition_id), current_actor_id())) WITH CHECK (perm_project_shaped(transition_project(transition_id), current_actor_id()));
CREATE POLICY transition_rule_removed ON transition_rule AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_shaped(transition_project(transition_id), current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON req_status, workflow, workflow_step, workflow_transition, transition_rule TO exciter_app, exciter_admin;

-- The status moves only through the functions above: the app role updates every column of a requirement but it.
REVOKE UPDATE ON requirement FROM exciter_app;
GRANT UPDATE (title, statement, rationale, attributes, revision, normative_hash, owner_id, priority, updated_by, updated_at,
              trashed_at, trashed_by, trash_id, type_id, parent_id, rank, copied_from, project_id, number, kind)
    ON requirement TO exciter_app;

-- +goose Down
GRANT UPDATE ON requirement TO exciter_app;
DROP TRIGGER IF EXISTS project_workflow_seed ON project;
DROP FUNCTION IF EXISTS project_workflow_seed();
DROP FUNCTION IF EXISTS project_workflow_default(uuid);
DROP FUNCTION IF EXISTS workflow_default_edge(uuid, uuid, text, uuid, uuid, integer, jsonb, boolean, boolean);
DROP FUNCTION IF EXISTS requirement_revision_status(uuid);
DROP FUNCTION IF EXISTS requirement_remap_status(uuid, uuid);
DROP FUNCTION IF EXISTS requirement_transition(uuid, uuid, text);
DROP FUNCTION IF EXISTS perm_project_roles(uuid, uuid);
DROP TRIGGER IF EXISTS requirement_status_initial ON requirement;
DROP FUNCTION IF EXISTS requirement_status_initial();
ALTER TABLE requirement DROP CONSTRAINT IF EXISTS requirement_section_statusless, DROP CONSTRAINT IF EXISTS requirement_status_fkey, DROP COLUMN IF EXISTS status_id;
ALTER TABLE requirement_type DROP CONSTRAINT IF EXISTS requirement_type_workflow_fkey, DROP COLUMN IF EXISTS workflow_id;
DROP TABLE IF EXISTS transition_rule;
DROP TABLE IF EXISTS workflow_transition;
DROP TABLE IF EXISTS workflow_step;
DROP TABLE IF EXISTS workflow;
DROP TABLE IF EXISTS req_status;
DROP FUNCTION IF EXISTS transition_project(uuid);
DROP FUNCTION IF EXISTS workflow_project(uuid);
DROP FUNCTION IF EXISTS requirement_workflow(uuid, uuid);
UPDATE project SET settings = settings - 'workflowId';
