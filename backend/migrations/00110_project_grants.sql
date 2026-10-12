-- +goose Up
-- Grants (#12): the role a person, a group or everyone holds in a project. What somebody may do there is the
-- union of every grant that reaches them, and perm_project_holds, which every project policy calls, now reads
-- them instead of the organization role alone.

-- A grant names a group by its organization too, so it cannot reach into another tenant's groups.
ALTER TABLE groups ADD CONSTRAINT groups_org_id_key UNIQUE (org_id, id);

CREATE TABLE project_grant (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id   uuid NOT NULL,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group', 'everyone')),
    user_id      uuid,
    group_id     uuid,
    role         text NOT NULL CHECK (role IN ('viewer', 'reviewer', 'editor', 'admin')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    -- Somebody who leaves the organization, or a group deleted, takes their grants along.
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, group_id) REFERENCES groups (org_id, id) ON DELETE CASCADE,
    CONSTRAINT project_grant_subject_shape CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL)
        OR (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL)
        OR (subject_type = 'everyone' AND user_id IS NULL AND group_id IS NULL)),
    CONSTRAINT project_grant_everyone_not_admin CHECK (subject_type <> 'everyone' OR role <> 'admin'),
    CONSTRAINT project_grant_one_per_subject UNIQUE NULLS NOT DISTINCT (project_id, subject_type, user_id, group_id)
);

CREATE INDEX project_grant_user_idx ON project_grant (org_id, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX project_grant_group_idx ON project_grant (org_id, group_id) WHERE group_id IS NOT NULL;

-- A guest is let into a project to work in it, never to run it, so no grant names a guest as admin. One that
-- reaches a guest through a group counts as editor, which perm_project_holds sees to.
-- +goose StatementBegin
CREATE FUNCTION project_grant_guest_check() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.role = 'admin' AND NEW.subject_type = 'user' AND EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id AND org_role = 'guest') THEN
        RAISE EXCEPTION 'A guest cannot be an administrator of a project. Make them an editor, or a member of the organization first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'project_grant_guest_not_admin';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_grant_guest_check BEFORE INSERT OR UPDATE ON project_grant
    FOR EACH ROW EXECUTE FUNCTION project_grant_guest_check();

-- The rights each role gives, the narrowest first; roles add up, so a person holds the union of theirs.
-- +goose StatementBegin
CREATE FUNCTION perm_role_rights(role text) RETURNS text[]
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT CASE role
        WHEN 'viewer'   THEN ARRAY['view']
        WHEN 'reviewer' THEN ARRAY['view', 'comment', 'approve']
        WHEN 'editor'   THEN ARRAY['view', 'comment', 'edit']
        WHEN 'admin'    THEN ARRAY['view', 'comment', 'edit', 'approve', 'administer']
        ELSE ARRAY[]::text[]
    END
$$;
-- +goose StatementEnd

-- Whether a grant's subject is the actor: them, a group they are in, or everyone, which leaves guests out.
-- +goose StatementBegin
CREATE FUNCTION perm_grant_reaches(actor uuid, actor_role text, subject_type text, user_id uuid, group_id uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE subject_type
        WHEN 'user' THEN user_id = actor
        WHEN 'group' THEN EXISTS (SELECT 1 FROM group_member gm WHERE gm.group_id = perm_grant_reaches.group_id AND gm.user_id = actor)
        WHEN 'everyone' THEN actor_role <> 'guest'
        ELSE false
    END
$$;
-- +goose StatementEnd

-- The role a grant gives the actor: a guest holds at most editor, whatever a group of theirs was given.
-- +goose StatementBegin
CREATE FUNCTION perm_grant_role(actor_role text, role text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT CASE WHEN actor_role = 'guest' AND role = 'admin' THEN 'editor' ELSE role END
$$;
-- +goose StatementEnd

-- What a person may do in a project: owners and administrators of the organization everything, so no project
-- is orphaned, and everybody else what the grants that reach them add up to. An archived project allows only
-- view, and administer, which is what unarchives and deletes it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_project_holds(project uuid, actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM project p
        JOIN org_member m ON m.org_id = p.org_id AND m.user_id = actor
        WHERE p.id = perm_project_holds.project AND p.org_id = current_org_id()
          AND (p.archived_at IS NULL OR permission IN ('view', 'administer'))
          AND (m.org_role IN ('owner', 'admin') OR EXISTS (
              SELECT 1 FROM project_grant g
              WHERE g.org_id = p.org_id AND g.project_id = p.id
                AND perm_grant_reaches(actor, m.org_role, g.subject_type, g.user_id, g.group_id)
                AND permission = ANY (perm_role_rights(perm_grant_role(m.org_role, g.role))))))
$$;
-- +goose StatementEnd

-- Whether a project takes changes, which an archived one does not until it is unarchived.
-- +goose StatementBegin
CREATE FUNCTION project_open(project uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM project p WHERE p.id = project_open.project AND p.org_id = current_org_id() AND p.archived_at IS NULL)
$$;
-- +goose StatementEnd

-- A new project is its maker's to run and everybody's to work in, whoever makes it and however; a template
-- may narrow that afterwards (#24).
-- +goose StatementBegin
CREATE FUNCTION project_grant_defaults() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.created_by AND org_role <> 'guest') THEN
        INSERT INTO project_grant (org_id, project_id, subject_type, user_id, role)
        VALUES (NEW.org_id, NEW.id, 'user', NEW.created_by, 'admin');
    END IF;
    INSERT INTO project_grant (org_id, project_id, subject_type, role) VALUES (NEW.org_id, NEW.id, 'everyone', 'editor');
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_grant_defaults AFTER INSERT ON project
    FOR EACH ROW EXECUTE FUNCTION project_grant_defaults();

-- Projects made before grants keep what they allowed: members edit, and their makers run them.
INSERT INTO project_grant (org_id, project_id, subject_type, user_id, role)
SELECT p.org_id, p.id, 'user', p.created_by, 'admin' FROM project p
JOIN org_member m ON m.org_id = p.org_id AND m.user_id = p.created_by AND m.org_role <> 'guest';
INSERT INTO project_grant (org_id, project_id, subject_type, role)
SELECT p.org_id, p.id, 'everyone', 'editor' FROM project p;

ALTER TABLE project_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_grant FORCE  ROW LEVEL SECURITY;

CREATE POLICY project_grant_tenant_isolation ON project_grant
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY project_grant_admin_bypass ON project_grant TO exciter_admin USING (true) WITH CHECK (true);

-- Through the app role a project's grants are read and changed by its administrators alone, and an archived
-- project's are left as they are until it is unarchived.
CREATE POLICY project_grant_read_by_administrators ON project_grant AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY project_grant_added_by_administrators ON project_grant AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id));
CREATE POLICY project_grant_changed_by_administrators ON project_grant AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id))
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id));
CREATE POLICY project_grant_removed_by_administrators ON project_grant AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id));

GRANT SELECT, INSERT, UPDATE, DELETE ON project_grant TO exciter_app, exciter_admin;

-- +goose Down
DROP TRIGGER IF EXISTS project_grant_defaults ON project;
DROP FUNCTION IF EXISTS project_grant_defaults();
DROP TABLE IF EXISTS project_grant;
DROP FUNCTION IF EXISTS project_grant_guest_check();
DROP FUNCTION IF EXISTS project_open(uuid);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_project_holds(project uuid, actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM project p
        JOIN org_member m ON m.org_id = p.org_id AND m.user_id = actor
        WHERE p.id = perm_project_holds.project AND p.org_id = current_org_id()
          AND (p.archived_at IS NULL OR permission IN ('view', 'administer'))
          AND CASE m.org_role
                  WHEN 'owner' THEN permission IN ('view', 'comment', 'edit', 'approve', 'administer')
                  WHEN 'admin' THEN permission IN ('view', 'comment', 'edit', 'approve', 'administer')
                  WHEN 'member' THEN permission IN ('view', 'comment', 'edit')
                  ELSE false
              END)
$$;
-- +goose StatementEnd
DROP FUNCTION IF EXISTS perm_grant_role(text, text);
DROP FUNCTION IF EXISTS perm_grant_reaches(uuid, text, text, uuid, uuid);
DROP FUNCTION IF EXISTS perm_role_rights(text);
ALTER TABLE groups DROP CONSTRAINT IF EXISTS groups_org_id_key;
