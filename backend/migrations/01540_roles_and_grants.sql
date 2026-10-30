-- +goose Up
-- Access through roles, a matrix, groups and grants, as the suite has it (#471).
--
-- A role is a named list of permissions; the built-in ones cannot be deleted,
-- and administrators add their own. A grant is a role, a scope (the whole
-- installation or one project) and a subject (a person or a group). What
-- somebody may do in a project is the union of the grants that reach them. A
-- built-in group, Everyone, holds every member and stands where the subject
-- "everyone" stood. Somebody's standing, member or guest, says only whether
-- they are in; administering the installation is a grant like any other.
--
-- Every project grant and group role of before moves to the grant of the
-- built-in role it amounts to, so nobody's access changes. The built-in
-- project roles keep the keys the workflow conditions and field rules name.
-- The policies keep calling the same functions, which now read the grants.

-- Not forced meanwhile, so that an owner that is no superuser reaches every row the backfill reads and writes.
ALTER TABLE app_user NO FORCE ROW LEVEL SECURITY;
ALTER TABLE groups NO FORCE ROW LEVEL SECURITY;
ALTER TABLE group_member NO FORCE ROW LEVEL SECURITY;
ALTER TABLE project NO FORCE ROW LEVEL SECURITY;
ALTER TABLE project_grant NO FORCE ROW LEVEL SECURITY;
ALTER TABLE oidc_group_role NO FORCE ROW LEVEL SECURITY;

CREATE TABLE role (
    key                    text PRIMARY KEY,
    name                   text NOT NULL,
    description            text NOT NULL DEFAULT '',
    -- A role that answers for the whole installation only, never one project.
    installation_wide_only boolean NOT NULL DEFAULT false,
    builtin                boolean NOT NULL DEFAULT false,
    permissions            text[] NOT NULL DEFAULT '{}',
    sort_order             integer NOT NULL DEFAULT 100,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT role_key_shape CHECK (key ~ '^[a-z][a-z0-9_]{1,39}$'),
    CONSTRAINT role_name_not_blank CHECK (btrim(name) <> ''),
    -- The permissions the code checks; a row naming another would grant a word nothing asks for.
    CONSTRAINT role_known_permissions CHECK (
        permissions <@ ARRAY['view', 'comment', 'edit', 'approve', 'administer', 'createProjects', 'administerInstallation'])
);

CREATE UNIQUE INDEX role_name_idx ON role (lower(name));

CREATE TRIGGER role_set_updated_at BEFORE UPDATE ON role
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO role (key, name, description, installation_wide_only, builtin, permissions, sort_order) VALUES
    ('administrator', 'Administrator',
     'Administers the installation: its people, roles, groups and settings, and every project in it.',
     true, true, ARRAY['view', 'comment', 'edit', 'approve', 'administer', 'createProjects', 'administerInstallation'], 10),
    ('admin', 'Project administrator',
     'Runs a project: its settings, grants, trash and archive, and works in it with every right.',
     false, true, ARRAY['view', 'comment', 'edit', 'approve', 'administer'], 20),
    ('editor', 'Editor',
     'Writes and changes requirements, and comments on them.',
     false, true, ARRAY['view', 'comment', 'edit'], 30),
    ('reviewer', 'Reviewer',
     'Reads requirements, comments on them and approves revisions.',
     false, true, ARRAY['view', 'comment', 'approve'], 40),
    ('viewer', 'Viewer',
     'Reads requirements, their history and their links, and changes nothing.',
     false, true, ARRAY['view'], 50);

-- A group mapped to member let its people in and gave them nothing more; a role that holds nothing carries that on.
INSERT INTO role (key, name, description, installation_wide_only, permissions, sort_order)
SELECT 'member', 'Member', 'Lets the people of a provider group in; what they may do comes from their other grants.', true, '{}', 60
WHERE EXISTS (SELECT 1 FROM oidc_group_role WHERE role = 'member');

CREATE TABLE role_assignment (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    role       text NOT NULL REFERENCES role (key) ON DELETE CASCADE,
    -- The project the role answers in; null for the whole installation.
    project_id uuid REFERENCES project (id) ON DELETE CASCADE,
    user_id    uuid REFERENCES app_user (id) ON DELETE CASCADE,
    group_id   uuid REFERENCES groups (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid REFERENCES app_user (id) ON DELETE SET NULL,
    CONSTRAINT role_assignment_one_subject CHECK ((user_id IS NULL) <> (group_id IS NULL))
);

-- The same grant twice is the one grant, the installation-wide ones too.
CREATE UNIQUE INDEX role_assignment_unique_idx ON role_assignment (role, project_id, user_id, group_id) NULLS NOT DISTINCT;
CREATE INDEX role_assignment_project_idx ON role_assignment (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX role_assignment_installation_idx ON role_assignment (role) WHERE project_id IS NULL;
CREATE INDEX role_assignment_user_idx ON role_assignment (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX role_assignment_group_idx ON role_assignment (group_id) WHERE group_id IS NOT NULL;

CREATE TRIGGER role_assignment_held_by_a_member BEFORE INSERT OR UPDATE OF user_id ON role_assignment
    FOR EACH ROW EXECUTE FUNCTION held_by_a_member('user_id');

-- Everyone: a group the installation keeps itself. A group somebody made under the name keeps its members and
-- grants under another.
ALTER TABLE groups ADD COLUMN builtin text CONSTRAINT groups_builtin_known CHECK (builtin IN ('everyone'));
CREATE UNIQUE INDEX groups_builtin_idx ON groups (builtin) WHERE builtin IS NOT NULL;

UPDATE groups SET name = name || ' (made by hand)' WHERE lower(name) = 'everyone';

-- A fixed id, so every copy of an installation, and every upgrade of one, names Everyone alike.
INSERT INTO groups (id, name, description, builtin)
VALUES ('00000000-0000-7000-8000-00000000e0e0', 'Everyone', 'Every member of the installation, not guests.', 'everyone');

-- What a built-in role is stays: it cannot go and its key cannot change, the administrator keeps administering
-- the installation, or an edit could lock every administrator out of the page that undoes it, and no role comes to
-- administer what a guest or Everyone holds it for.
-- +goose StatementBegin
CREATE FUNCTION role_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.builtin THEN
            RAISE EXCEPTION 'A built-in role cannot be deleted. Take its grants away instead.'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'role_builtin_stays';
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE' AND (NEW.key <> OLD.key OR NEW.builtin <> OLD.builtin) THEN
        RAISE EXCEPTION 'A role keeps its key and whether it is built in.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_key_fixed';
    END IF;
    IF NEW.key = 'administrator' AND NOT ('administerInstallation' = ANY (NEW.permissions)) THEN
        RAISE EXCEPTION 'The administrator keeps administering Exciter, or nobody could undo this.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_administrator_administers';
    END IF;
    IF TG_OP = 'UPDATE' AND 'administer' = ANY (NEW.permissions) AND NOT ('administer' = ANY (OLD.permissions))
       AND EXISTS (SELECT 1 FROM role_assignment a JOIN app_user u ON u.id = a.user_id
                   WHERE a.role = NEW.key AND u.role = 'guest') THEN
        RAISE EXCEPTION 'A guest holds this role, and a guest never administers a project. Take the role from the guests first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_guest_not_administrator';
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.permissions && ARRAY['administer', 'administerInstallation']
       AND NOT (OLD.permissions && ARRAY['administer', 'administerInstallation'])
       AND EXISTS (SELECT 1 FROM role_assignment a JOIN groups g ON g.id = a.group_id
                   WHERE a.role = NEW.key AND g.builtin = 'everyone') THEN
        RAISE EXCEPTION 'Everyone holds this role, so it cannot administer. Take the role from Everyone first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_everyone_administers';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER role_check BEFORE INSERT OR UPDATE OR DELETE ON role
    FOR EACH ROW EXECUTE FUNCTION role_guard();

-- A guest is let into the projects they are granted, to work in them, never to run one, and is granted nothing
-- across the installation; Everyone administers nothing, or every member would; a role for the whole installation
-- is not granted in one project.
-- +goose StatementBegin
CREATE FUNCTION role_assignment_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    perms text[];
    whole boolean;
BEGIN
    SELECT permissions, installation_wide_only INTO perms, whole FROM role WHERE key = NEW.role;
    IF NEW.project_id IS NOT NULL AND whole THEN
        RAISE EXCEPTION 'That role answers for the whole installation and cannot be granted in one project. Grant it without a project.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_installation_wide';
    END IF;
    IF NEW.user_id IS NOT NULL AND EXISTS (SELECT 1 FROM app_user WHERE id = NEW.user_id AND role = 'guest') THEN
        IF NEW.project_id IS NULL THEN
            RAISE EXCEPTION 'A guest is let into projects one at a time. Grant them a role in a project, or make them a member first.'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_guest_in_a_project';
        END IF;
        IF 'administer' = ANY (perms) THEN
            RAISE EXCEPTION 'A guest cannot be an administrator of a project. Make them an editor, or a member first.'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_guest_not_administrator';
        END IF;
    END IF;
    IF NEW.group_id IS NOT NULL AND perms && ARRAY['administer', 'administerInstallation']
       AND EXISTS (SELECT 1 FROM groups WHERE id = NEW.group_id AND builtin = 'everyone') THEN
        RAISE EXCEPTION 'Everyone cannot be given a role that administers, or every member would. Grant it to people or a group of their own.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_everyone_administers';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER role_assignment_check BEFORE INSERT OR UPDATE ON role_assignment
    FOR EACH ROW EXECUTE FUNCTION role_assignment_guard();

-- The grants of before. A project grant becomes the grant of the built-in role of the same key, everyone's the
-- Everyone group's.
INSERT INTO role_assignment (role, project_id, user_id, group_id, created_at)
SELECT g.role, g.project_id, g.user_id,
       CASE WHEN g.subject_type = 'everyone' THEN '00000000-0000-7000-8000-00000000e0e0'::uuid ELSE g.group_id END,
       g.created_at
FROM project_grant g
ON CONFLICT DO NOTHING;

-- A provider group mapped to a role becomes a group with the provider's name in its claim, granted the role it
-- amounts to across the installation: admin the administrator, member the member role, which lets its people in
-- as it did.
INSERT INTO groups (name, source, external_ref)
SELECT CASE WHEN EXISTS (SELECT 1 FROM groups o WHERE lower(o.name) = lower(r.group_ref))
            THEN r.group_ref || ' (provider)' ELSE r.group_ref END,
       'oidc', r.group_ref
FROM oidc_group_role r
WHERE NOT EXISTS (SELECT 1 FROM groups g WHERE g.external_ref = r.group_ref);

INSERT INTO role_assignment (role, group_id)
SELECT CASE r.role WHEN 'admin' THEN 'administrator' ELSE 'member' END, g.id
FROM oidc_group_role r JOIN groups g ON g.external_ref = r.group_ref
ON CONFLICT DO NOTHING;

-- Owners and administrators keep administering: by name, unless a group they are in already does so for them, as
-- one that followed the provider's groups did.
INSERT INTO role_assignment (role, user_id)
SELECT 'administrator', u.id FROM app_user u
WHERE u.role IN ('owner', 'admin')
  AND (u.role_source = 'manual' OR NOT EXISTS (
      SELECT 1 FROM role_assignment a JOIN group_member gm ON gm.group_id = a.group_id
      WHERE a.role = 'administrator' AND a.project_id IS NULL AND gm.user_id = u.id))
ON CONFLICT DO NOTHING;

-- Standing says only whether somebody is in.
DROP TRIGGER app_user_keeps_an_administrator ON app_user;
DROP FUNCTION app_user_keeps_an_administrator();
DROP TRIGGER app_user_role_guard ON app_user;
DROP POLICY app_user_admins_add ON app_user;
ALTER TABLE app_user DROP CONSTRAINT app_user_owner_is_manual;
ALTER TABLE app_user DROP CONSTRAINT app_user_role_check;
-- The role is not an edit of the account, so its updated_at stays.
ALTER TABLE app_user DISABLE TRIGGER app_user_set_updated_at;
UPDATE app_user SET role = 'member' WHERE role IN ('owner', 'admin');
ALTER TABLE app_user ENABLE TRIGGER app_user_set_updated_at;
ALTER TABLE app_user DROP COLUMN role_source;
ALTER TABLE app_user ADD CONSTRAINT app_user_role_check CHECK (role IN ('member', 'guest'));

CREATE POLICY app_user_admins_add ON app_user AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (current_actor_administers());

-- A member stays a member through the app role: an administrator switches an account off instead.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_user_role_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    was text := CASE WHEN TG_OP = 'UPDATE' THEN OLD.role END;
BEGIN
    IF current_user = 'exciter_app' AND was IS NOT NULL AND NEW.role IS NULL THEN
        RAISE EXCEPTION 'a member stays a member; switch the account off instead' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.role IS NULL THEN
        NEW.joined_at := NULL;
    ELSIF was IS NULL THEN
        NEW.joined_at := COALESCE(NEW.joined_at, now());
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER app_user_role_guard BEFORE INSERT OR UPDATE OF role ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_role_guard();

INSERT INTO group_member (group_id, user_id)
SELECT '00000000-0000-7000-8000-00000000e0e0', u.id FROM app_user u WHERE u.role = 'member';

-- Everyone follows the standing: a member is in it, a guest or somebody who left is not.
-- +goose StatementBegin
CREATE FUNCTION app_user_joins_everyone() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.role = 'member' THEN
        INSERT INTO group_member (group_id, user_id)
        SELECT id, NEW.id FROM groups WHERE builtin = 'everyone'
        ON CONFLICT DO NOTHING;
    ELSE
        DELETE FROM group_member m USING groups g
        WHERE m.group_id = g.id AND g.builtin = 'everyone' AND m.user_id = NEW.id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER app_user_joins_everyone AFTER INSERT OR UPDATE OF role ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_joins_everyone();

-- A member still here stays in Everyone, whoever asks.
-- +goose StatementBegin
CREATE FUNCTION group_member_keeps_everyone() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM groups WHERE id = OLD.group_id AND builtin = 'everyone')
       AND EXISTS (SELECT 1 FROM app_user WHERE id = OLD.user_id AND role = 'member') THEN
        RAISE EXCEPTION 'Everyone holds every member, so nobody is taken out of it by hand.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'group_member_keeps_everyone';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER group_member_keeps_everyone BEFORE DELETE ON group_member
    FOR EACH ROW EXECUTE FUNCTION group_member_keeps_everyone();

-- Nobody but a member joins Everyone.
-- +goose StatementBegin
CREATE FUNCTION group_member_everyone_members() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM groups WHERE id = NEW.group_id AND builtin = 'everyone')
       AND NOT EXISTS (SELECT 1 FROM app_user WHERE id = NEW.user_id AND role = 'member') THEN
        RAISE EXCEPTION 'Everyone holds the members of the installation and nobody else.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'group_member_everyone_members';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER group_member_everyone_members BEFORE INSERT OR UPDATE ON group_member
    FOR EACH ROW EXECUTE FUNCTION group_member_everyone_members();

-- +goose StatementBegin
CREATE FUNCTION groups_builtin_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.builtin IS NOT NULL THEN
            RAISE EXCEPTION 'The group % is built in and cannot be deleted.', OLD.name
                USING ERRCODE = 'check_violation', CONSTRAINT = 'groups_builtin_fixed';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.builtin IS DISTINCT FROM OLD.builtin
       OR (OLD.builtin IS NOT NULL AND (NEW.name, NEW.description, NEW.source, NEW.external_ref)
           IS DISTINCT FROM (OLD.name, OLD.description, OLD.source, OLD.external_ref)) THEN
        RAISE EXCEPTION 'The group % is built in and cannot be changed.', OLD.name
            USING ERRCODE = 'check_violation', CONSTRAINT = 'groups_builtin_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER groups_builtin_guard BEFORE UPDATE OR DELETE ON groups
    FOR EACH ROW EXECUTE FUNCTION groups_builtin_guard();

DROP TRIGGER project_grant_defaults ON project;
DROP FUNCTION project_grant_defaults();
DROP TABLE project_grant;
DROP FUNCTION project_grant_guest_check();
DROP TRIGGER oidc_provider_forgets_group_roles ON oidc_provider;
DROP FUNCTION oidc_provider_forgets_group_roles();
DROP TABLE oidc_group_role;

-- The rules, as the policies read them.

-- What a grant gives somebody of a standing: a guest a role to run the project reaches works in it as an editor
-- does, as before, and never runs it.
-- +goose StatementBegin
CREATE FUNCTION perm_role_held(standing text, permissions text[]) RETURNS text[]
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT CASE WHEN standing = 'guest' AND 'administer' = ANY (permissions)
                THEN array_remove(array_remove(permissions, 'administer'), 'approve')
                ELSE permissions END
$$;
-- +goose StatementEnd

-- The permissions the grants across the installation give a member, their own and their groups'; a guest is
-- granted nothing across it.
-- +goose StatementBegin
CREATE FUNCTION perm_installation_grants(actor uuid) RETURNS text[]
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(array_agg(DISTINCT p.word ORDER BY p.word), '{}')
    FROM role_assignment a
    JOIN role r ON r.key = a.role
    CROSS JOIN LATERAL unnest(r.permissions) AS p (word)
    WHERE a.project_id IS NULL
      AND EXISTS (SELECT 1 FROM app_user m WHERE m.id = actor AND m.role = 'member')
      AND (a.user_id = actor OR (a.group_id IS NOT NULL AND EXISTS (
          SELECT 1 FROM group_member gm WHERE gm.group_id = a.group_id AND gm.user_id = actor)))
$$;
-- +goose StatementEnd

-- Whether a grant lets somebody administer the installation.
-- +goose StatementBegin
CREATE FUNCTION perm_is_administrator(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT 'administerInstallation' = ANY (perm_installation_grants(actor))
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_actor_administers() RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_token_whole(current_actor_id()) AND perm_is_administrator(current_actor_id())
$$;
-- +goose StatementEnd

-- Whether a grant lets somebody make projects, which a token limited to projects does not.
-- +goose StatementBegin
CREATE FUNCTION current_actor_creates_projects() RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_token_whole(current_actor_id()) AND 'createProjects' = ANY (perm_installation_grants(current_actor_id()))
$$;
-- +goose StatementEnd

ALTER POLICY project_made_by_admins ON project
    WITH CHECK (current_actor_creates_projects() AND created_by = current_actor_id());

-- What a person may do in a project: what the grants that reach them add up to, those in the project and, for a
-- member, those across the installation. An archived project allows only view, and administer, which is what
-- unarchives and deletes it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_project_holds(project uuid, actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_token_reaches(actor, perm_project_holds.project) AND EXISTS (
        SELECT 1 FROM project p
        JOIN app_user m ON m.id = actor AND m.role IS NOT NULL
        WHERE p.id = perm_project_holds.project AND (p.archived_at IS NULL OR permission IN ('view', 'administer'))
          AND EXISTS (
              SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
              WHERE (a.project_id = p.id OR (a.project_id IS NULL AND m.role = 'member'))
                AND (a.user_id = actor OR (a.group_id IS NOT NULL AND EXISTS (
                    SELECT 1 FROM group_member gm WHERE gm.group_id = a.group_id AND gm.user_id = actor)))
                AND permission = ANY (perm_role_held(m.role, r.permissions))))
$$;
-- +goose StatementEnd

-- The project roles a person holds, as the grants that reach them give them: what a role condition and a field
-- rule ask. Administering the installation holds admin in every project, and a guest a role to run the project
-- reaches holds editor, as before.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_project_roles(target uuid, actor uuid) RETURNS text[]
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(array_agg(DISTINCT role), '{}') FROM (
        SELECT 'admin' AS role
        WHERE perm_is_administrator(actor) AND EXISTS (SELECT 1 FROM project p WHERE p.id = target)
        UNION ALL
        SELECT CASE WHEN m.role = 'guest' AND 'administer' = ANY (r.permissions) THEN 'editor' ELSE a.role END
        FROM role_assignment a
        JOIN role r ON r.key = a.role
        JOIN app_user m ON m.id = actor AND m.role IS NOT NULL
        WHERE (a.project_id = target OR (a.project_id IS NULL AND m.role = 'member' AND NOT r.installation_wide_only
                                         AND EXISTS (SELECT 1 FROM project p WHERE p.id = target)))
          AND (a.user_id = actor OR (a.group_id IS NOT NULL AND EXISTS (
              SELECT 1 FROM group_member gm WHERE gm.group_id = a.group_id AND gm.user_id = actor)))
    ) roles
$$;
-- +goose StatementEnd

-- The grants that reach a person in a project and across the installation, for the access inspector.
-- +goose StatementBegin
CREATE FUNCTION perm_grants_reaching(person uuid, target uuid)
    RETURNS TABLE (id uuid, role text, project_id uuid, user_id uuid, group_id uuid, created_at timestamptz)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT a.id, a.role, a.project_id, a.user_id, a.group_id, a.created_at
    FROM role_assignment a
    JOIN app_user m ON m.id = person AND m.role IS NOT NULL
    WHERE (a.project_id = target OR (a.project_id IS NULL AND m.role = 'member'))
      AND (a.user_id = person OR (a.group_id IS NOT NULL AND EXISTS (
          SELECT 1 FROM group_member gm WHERE gm.group_id = a.group_id AND gm.user_id = person)))
      AND (person = current_actor_id() OR current_actor_administers()
           OR perm_project_holds(target, current_actor_id(), 'administer'))
$$;
-- +goose StatementEnd

-- A new project is its maker's to run and every member's to work in, whoever makes it and however; a template
-- may narrow that afterwards (#24).
-- +goose StatementBegin
CREATE FUNCTION project_default_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.created_by AND role = 'member') THEN
        INSERT INTO role_assignment (role, project_id, user_id, created_by)
        VALUES ('admin', NEW.id, NEW.created_by, NEW.created_by);
    END IF;
    INSERT INTO role_assignment (role, project_id, group_id)
    SELECT 'editor', NEW.id, id FROM groups WHERE builtin = 'everyone';
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_default_grants AFTER INSERT ON project
    FOR EACH ROW EXECUTE FUNCTION project_default_grants();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_user_left() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM api_token WHERE user_id = NEW.id;
    DELETE FROM armature_token WHERE user_id = NEW.id;
    DELETE FROM stator_token WHERE user_id = NEW.id;
    DELETE FROM role_assignment WHERE user_id = NEW.id;
    UPDATE requirement SET owner_id = NULL WHERE owner_id = NEW.id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- The installation keeps an active administrator: a member switched on who holds administering it through a
-- grant of their own or of a group. Service code asks first, so its refusal is a sentence.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION administered_without(leaving uuid) RETURNS boolean
    LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('administered_without'));
    RETURN EXISTS (SELECT 1 FROM app_user
                   WHERE id <> leaving AND role = 'member' AND is_active AND perm_is_administrator(id));
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION perm_active_administrators() RETURNS bigint
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT count(*) FROM app_user u WHERE u.role = 'member' AND u.is_active AND perm_is_administrator(u.id)
$$;
-- +goose StatementEnd

-- A grant, a role or a group membership is weighed at commit, so a change that hands administering on in the same
-- transaction passes. Only a change that took administering from somebody is weighed: an installation that has no
-- administrator yet, as a fresh one or a test's, still lets everybody else come and go.
-- +goose StatementBegin
CREATE FUNCTION installation_keeps_administrator() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    took boolean;
BEGIN
    IF TG_TABLE_NAME = 'role_assignment' THEN
        took := NOT EXISTS (SELECT 1 FROM role r WHERE r.key = OLD.role AND NOT ('administerInstallation' = ANY (r.permissions)));
    ELSIF TG_TABLE_NAME = 'group_member' THEN
        took := EXISTS (SELECT 1 FROM app_user u WHERE u.id = OLD.user_id AND u.role = 'member' AND u.is_active)
            AND EXISTS (SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
                        WHERE a.group_id = OLD.group_id AND a.project_id IS NULL AND 'administerInstallation' = ANY (r.permissions));
    ELSE
        took := true;
    END IF;
    IF NOT took THEN
        RETURN NULL;
    END IF;
    PERFORM pg_advisory_xact_lock(hashtext('administered_without'));
    IF perm_active_administrators() > 0 OR NOT EXISTS (SELECT 1 FROM app_user WHERE role = 'member') THEN
        RETURN NULL;
    END IF;
    RAISE EXCEPTION 'Exciter keeps at least one active administrator. Make somebody else an administrator first, then take it from this one.'
        USING ERRCODE = 'check_violation', CONSTRAINT = 'installation_keeps_administrator';
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER installation_keeps_administrator
    AFTER DELETE OR UPDATE ON role_assignment
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW WHEN (OLD.project_id IS NULL)
    EXECUTE FUNCTION installation_keeps_administrator();
CREATE CONSTRAINT TRIGGER installation_keeps_administrator
    AFTER UPDATE OF permissions OR DELETE ON role
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW WHEN ('administerInstallation' = ANY (OLD.permissions))
    EXECUTE FUNCTION installation_keeps_administrator();
CREATE CONSTRAINT TRIGGER installation_keeps_administrator
    AFTER DELETE ON group_member
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION installation_keeps_administrator();

-- An account is weighed at once, under the lock administered_without takes, so two administrators switching each
-- other off do not both get through: the second waits for the first and is refused.
-- +goose StatementBegin
CREATE FUNCTION app_user_keeps_an_administrator() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.role = 'member' AND OLD.is_active
       AND (TG_OP = 'DELETE' OR NOT NEW.is_active OR NEW.role IS DISTINCT FROM 'member')
       AND perm_is_administrator(OLD.id) AND NOT administered_without(OLD.id) THEN
        IF TG_OP = 'UPDATE' AND NOT NEW.is_active THEN
            RAISE EXCEPTION 'the last active administrator cannot be switched off' USING ERRCODE = 'check_violation';
        END IF;
        RAISE EXCEPTION 'the installation keeps its last active administrator' USING ERRCODE = 'check_violation';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER app_user_keeps_an_administrator BEFORE DELETE OR UPDATE OF is_active, role ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_keeps_an_administrator();

-- The audit log is the installation's administrators' to read, as it was.
ALTER POLICY audit_log_admins_read ON audit_log USING (perm_is_administrator(current_actor_id()));

-- Every member reads the roles; the installation's administrators change them.
ALTER TABLE role ENABLE ROW LEVEL SECURITY;
ALTER TABLE role FORCE ROW LEVEL SECURITY;
CREATE POLICY role_installation ON role USING (true) WITH CHECK (true);
CREATE POLICY role_admin_bypass ON role TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY role_admins_add ON role AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (current_actor_administers());
CREATE POLICY role_admins_change ON role AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());
CREATE POLICY role_admins_remove ON role AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (current_actor_administers());

-- A grant across the installation is its administrators' to read and give; a grant in a project, the project's
-- administrators' too, and an archived project's are left as they are until it is unarchived.
ALTER TABLE role_assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_assignment FORCE ROW LEVEL SECURITY;
CREATE POLICY role_assignment_installation ON role_assignment USING (true) WITH CHECK (true);
CREATE POLICY role_assignment_admin_bypass ON role_assignment TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY role_assignment_administrators ON role_assignment AS RESTRICTIVE TO exciter_app
    USING (current_actor_administers()
           OR (project_id IS NOT NULL AND perm_project_holds(project_id, current_actor_id(), 'administer')))
    WITH CHECK (current_actor_administers()
           OR (project_id IS NOT NULL AND perm_project_holds(project_id, current_actor_id(), 'administer')));
CREATE POLICY role_assignment_added_while_open ON role_assignment AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (project_id IS NULL OR project_open(project_id));
CREATE POLICY role_assignment_changed_while_open ON role_assignment AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (project_id IS NULL OR project_open(project_id)) WITH CHECK (project_id IS NULL OR project_open(project_id));
CREATE POLICY role_assignment_removed_while_open ON role_assignment AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (project_id IS NULL OR project_open(project_id));

GRANT SELECT, INSERT, UPDATE, DELETE ON role, role_assignment TO exciter_app, exciter_admin;

DROP FUNCTION perm_grant_reaches(uuid, text, text, uuid, uuid);
DROP FUNCTION perm_grant_role(text, text);
DROP FUNCTION perm_role_rights(text);

ALTER TABLE project FORCE ROW LEVEL SECURITY;
ALTER TABLE group_member FORCE ROW LEVEL SECURITY;
ALTER TABLE groups FORCE ROW LEVEL SECURITY;
ALTER TABLE app_user FORCE ROW LEVEL SECURITY;

-- +goose Down
ALTER TABLE app_user NO FORCE ROW LEVEL SECURITY;
ALTER TABLE groups NO FORCE ROW LEVEL SECURITY;
ALTER TABLE group_member NO FORCE ROW LEVEL SECURITY;
ALTER TABLE project NO FORCE ROW LEVEL SECURITY;
ALTER TABLE role NO FORCE ROW LEVEL SECURITY;
ALTER TABLE role_assignment NO FORCE ROW LEVEL SECURITY;

-- +goose StatementBegin
CREATE FUNCTION public.perm_grant_reaches(actor uuid, actor_role text, subject_type text, user_id uuid, group_id uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
    AS $$
    SELECT CASE subject_type
        WHEN 'user' THEN user_id = actor
        WHEN 'group' THEN EXISTS (SELECT 1 FROM group_member gm WHERE gm.group_id = perm_grant_reaches.group_id AND gm.user_id = actor)
        WHEN 'everyone' THEN actor_role <> 'guest'
        ELSE false
    END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.perm_grant_role(actor_role text, role text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT CASE WHEN actor_role = 'guest' AND role = 'admin' THEN 'editor' ELSE role END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.perm_role_rights(role text) RETURNS text[]
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

-- The standing of before: administering the installation by name is admin, through a group alone the provider's
-- admin, and the first administrator the owner.
ALTER TABLE app_user ADD COLUMN role_source text DEFAULT 'manual'::text NOT NULL;
ALTER TABLE app_user ADD CONSTRAINT app_user_role_source_check CHECK ((role_source = ANY (ARRAY['manual'::text, 'oidc'::text])));
ALTER TABLE app_user DROP CONSTRAINT app_user_role_check;
ALTER TABLE app_user ADD CONSTRAINT app_user_role_check CHECK ((role = ANY (ARRAY['owner'::text, 'admin'::text, 'member'::text, 'guest'::text])));
DROP TRIGGER app_user_keeps_an_administrator ON app_user;
DROP FUNCTION app_user_keeps_an_administrator();
DROP TRIGGER app_user_joins_everyone ON app_user;
DROP TRIGGER app_user_role_guard ON app_user;
ALTER TABLE app_user DISABLE TRIGGER app_user_set_updated_at;
UPDATE app_user u SET role = 'admin',
       role_source = CASE WHEN EXISTS (
           SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
           WHERE a.project_id IS NULL AND a.user_id = u.id AND 'administerInstallation' = ANY (r.permissions))
           THEN 'manual' ELSE 'oidc' END
WHERE u.role = 'member' AND perm_is_administrator(u.id);
UPDATE app_user SET role = 'owner', role_source = 'manual'
WHERE id = (SELECT id FROM app_user WHERE role = 'admin' ORDER BY role_source = 'oidc', joined_at, id LIMIT 1);
ALTER TABLE app_user ENABLE TRIGGER app_user_set_updated_at;
ALTER TABLE app_user ADD CONSTRAINT app_user_owner_is_manual CHECK (((role IS DISTINCT FROM 'owner'::text) OR (role_source = 'manual'::text)));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.app_user_role_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    was text := CASE WHEN TG_OP = 'UPDATE' THEN OLD.role END;
BEGIN
    IF current_user = 'exciter_app' AND was IS NOT NULL AND NEW.role IS NULL THEN
        RAISE EXCEPTION 'a member stays a member; switch the account off instead' USING ERRCODE = 'check_violation';
    END IF;
    IF current_user = 'exciter_app' AND NEW.role IS DISTINCT FROM was AND 'owner' IN (NEW.role, was) THEN
        RAISE EXCEPTION 'an owner''s role is not changed here' USING ERRCODE = 'check_violation';
    END IF;
    IF was = 'owner' AND NEW.role <> 'owner' AND NEW.role_source = 'oidc' THEN
        RAISE EXCEPTION 'the identity provider cannot change the role of an owner' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.role IS NULL THEN
        NEW.joined_at := NULL;
    ELSIF was IS NULL THEN
        NEW.joined_at := COALESCE(NEW.joined_at, now());
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER app_user_role_guard BEFORE INSERT OR UPDATE OF role, role_source ON public.app_user FOR EACH ROW EXECUTE FUNCTION public.app_user_role_guard();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.administered_without(leaving uuid) RETURNS boolean
    LANGUAGE plpgsql
    AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('administered_without'));
    RETURN EXISTS (SELECT 1 FROM app_user
                   WHERE id <> leaving AND role IN ('owner', 'admin') AND is_active);
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.app_user_keeps_an_administrator() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.role IN ('owner', 'admin') AND OLD.is_active
       AND (TG_OP = 'DELETE' OR NOT NEW.is_active OR NEW.role IS NULL OR NEW.role NOT IN ('owner', 'admin'))
       AND NOT administered_without(OLD.id) THEN
        IF TG_OP = 'UPDATE' AND NOT NEW.is_active THEN
            RAISE EXCEPTION 'the last active administrator cannot be switched off' USING ERRCODE = 'check_violation';
        END IF;
        RAISE EXCEPTION 'the installation keeps its last active administrator' USING ERRCODE = 'check_violation';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER app_user_keeps_an_administrator BEFORE DELETE OR UPDATE OF is_active, role ON public.app_user FOR EACH ROW EXECUTE FUNCTION public.app_user_keeps_an_administrator();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.current_actor_administers() RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
    AS $$
    SELECT perm_token_whole(current_actor_id()) AND EXISTS (
        SELECT 1 FROM app_user
        WHERE id = current_actor_id() AND role IN ('owner', 'admin'))
$$;
-- +goose StatementEnd

DROP POLICY app_user_admins_add ON app_user;
CREATE POLICY app_user_admins_add ON public.app_user AS RESTRICTIVE FOR INSERT TO exciter_app WITH CHECK ((public.current_actor_administers() AND (role IS DISTINCT FROM 'owner'::text)));
ALTER POLICY audit_log_admins_read ON audit_log USING ((EXISTS ( SELECT 1
   FROM public.app_user
  WHERE ((app_user.id = public.current_actor_id()) AND (app_user.role = ANY (ARRAY['owner'::text, 'admin'::text]))))));
ALTER POLICY project_made_by_admins ON project
    WITH CHECK ((public.current_actor_administers() AND (created_by = public.current_actor_id())));

CREATE TABLE public.project_grant (
    id uuid DEFAULT uuidv7() NOT NULL,
    project_id uuid NOT NULL,
    subject_type text NOT NULL,
    user_id uuid,
    group_id uuid,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT project_grant_everyone_not_admin CHECK (((subject_type <> 'everyone'::text) OR (role <> 'admin'::text))),
    CONSTRAINT project_grant_role_check CHECK ((role = ANY (ARRAY['viewer'::text, 'reviewer'::text, 'editor'::text, 'admin'::text]))),
    CONSTRAINT project_grant_subject_shape CHECK ((((subject_type = 'user'::text) AND (user_id IS NOT NULL) AND (group_id IS NULL)) OR ((subject_type = 'group'::text) AND (group_id IS NOT NULL) AND (user_id IS NULL)) OR ((subject_type = 'everyone'::text) AND (user_id IS NULL) AND (group_id IS NULL)))),
    CONSTRAINT project_grant_subject_type_check CHECK ((subject_type = ANY (ARRAY['user'::text, 'group'::text, 'everyone'::text])))
);
ALTER TABLE ONLY public.project_grant
    ADD CONSTRAINT project_grant_one_per_subject UNIQUE NULLS NOT DISTINCT (project_id, subject_type, user_id, group_id);
ALTER TABLE ONLY public.project_grant
    ADD CONSTRAINT project_grant_pkey PRIMARY KEY (id);
CREATE INDEX project_grant_group_idx ON public.project_grant USING btree (group_id) WHERE (group_id IS NOT NULL);
CREATE INDEX project_grant_user_idx ON public.project_grant USING btree (user_id) WHERE (user_id IS NOT NULL);
ALTER TABLE ONLY public.project_grant
    ADD CONSTRAINT project_grant_group_id_fkey FOREIGN KEY (group_id) REFERENCES public.groups(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.project_grant
    ADD CONSTRAINT project_grant_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.project(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.project_grant
    ADD CONSTRAINT project_grant_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.app_user(id) ON DELETE CASCADE;

-- Each subject keeps the widest built-in role its grants in a project amount to; Everyone is everyone again, and
-- runs no project.
INSERT INTO public.project_grant (project_id, subject_type, user_id, group_id, role, created_at)
SELECT project_id, subject_type, user_id, group_id,
       CASE WHEN subject_type = 'everyone' AND rank = 4 THEN 'editor'
            ELSE (ARRAY['viewer', 'reviewer', 'editor', 'admin'])[rank] END,
       created_at
FROM (
    SELECT a.project_id,
           CASE WHEN g.builtin = 'everyone' THEN 'everyone' WHEN a.user_id IS NOT NULL THEN 'user' ELSE 'group' END AS subject_type,
           a.user_id, CASE WHEN g.builtin = 'everyone' THEN NULL ELSE a.group_id END AS group_id,
           max(CASE WHEN 'administer' = ANY (r.permissions) THEN 4 WHEN 'edit' = ANY (r.permissions) THEN 3
                    WHEN 'approve' = ANY (r.permissions) THEN 2 ELSE 1 END) AS rank,
           min(a.created_at) AS created_at
    FROM role_assignment a
    JOIN role r ON r.key = a.role
    LEFT JOIN groups g ON g.id = a.group_id
    WHERE a.project_id IS NOT NULL AND r.permissions && ARRAY['view', 'comment', 'edit', 'approve', 'administer']
    GROUP BY 1, 2, 3, 4
) widest;

ALTER TABLE ONLY public.project_grant FORCE ROW LEVEL SECURITY;
ALTER TABLE public.project_grant ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_grant_added_by_administrators ON public.project_grant AS RESTRICTIVE FOR INSERT TO exciter_app WITH CHECK ((public.perm_project_holds(project_id, public.current_actor_id(), 'administer'::text) AND public.project_open(project_id)));
CREATE POLICY project_grant_changed_by_administrators ON public.project_grant AS RESTRICTIVE FOR UPDATE TO exciter_app USING ((public.perm_project_holds(project_id, public.current_actor_id(), 'administer'::text) AND public.project_open(project_id))) WITH CHECK ((public.perm_project_holds(project_id, public.current_actor_id(), 'administer'::text) AND public.project_open(project_id)));
CREATE POLICY project_grant_installation ON public.project_grant USING (true) WITH CHECK (true);
CREATE POLICY project_grant_read_by_administrators ON public.project_grant AS RESTRICTIVE FOR SELECT TO exciter_app USING (public.perm_project_holds(project_id, public.current_actor_id(), 'administer'::text));
CREATE POLICY project_grant_removed_by_administrators ON public.project_grant AS RESTRICTIVE FOR DELETE TO exciter_app USING ((public.perm_project_holds(project_id, public.current_actor_id(), 'administer'::text) AND public.project_open(project_id)));
GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.project_grant TO exciter_app;
GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.project_grant TO exciter_admin;

-- +goose StatementBegin
CREATE FUNCTION public.project_grant_guest_check() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
    AS $$
BEGIN
    IF NEW.role = 'admin' AND NEW.subject_type = 'user' AND EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.user_id AND role = 'guest') THEN
        RAISE EXCEPTION 'A guest cannot be an administrator of a project. Make them an editor, or a member first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'project_grant_guest_not_admin';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_grant_guest_check BEFORE INSERT OR UPDATE ON public.project_grant FOR EACH ROW EXECUTE FUNCTION public.project_grant_guest_check();
CREATE TRIGGER project_grant_held_by_a_member BEFORE INSERT OR UPDATE OF user_id ON public.project_grant FOR EACH ROW EXECUTE FUNCTION public.held_by_a_member('user_id');

-- +goose StatementBegin
CREATE FUNCTION public.project_grant_defaults() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
    AS $$
BEGIN
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.created_by AND role <> 'guest') THEN
        INSERT INTO project_grant (project_id, subject_type, user_id, role)
        VALUES (NEW.id, 'user', NEW.created_by, 'admin');
    END IF;
    INSERT INTO project_grant (project_id, subject_type, role) VALUES (NEW.id, 'everyone', 'editor');
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER project_default_grants ON project;
DROP FUNCTION project_default_grants();
CREATE TRIGGER project_grant_defaults AFTER INSERT ON public.project FOR EACH ROW EXECUTE FUNCTION public.project_grant_defaults();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_project_holds(project uuid, actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
    AS $$
    SELECT perm_token_reaches(actor, perm_project_holds.project) AND EXISTS (
        SELECT 1 FROM project p
        JOIN app_user m ON m.id = actor AND m.role IS NOT NULL
        WHERE p.id = perm_project_holds.project AND (p.archived_at IS NULL OR permission IN ('view', 'administer'))
          AND (m.role IN ('owner', 'admin') OR EXISTS (
              SELECT 1 FROM project_grant g
              WHERE g.project_id = p.id
                AND perm_grant_reaches(actor, m.role, g.subject_type, g.user_id, g.group_id)
                AND permission = ANY (perm_role_rights(perm_grant_role(m.role, g.role))))))
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_project_roles(target uuid, actor uuid) RETURNS text[]
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
    AS $$
    SELECT COALESCE(array_agg(DISTINCT role), '{}') FROM (
        SELECT 'admin' AS role FROM app_user m
        WHERE m.id = actor AND m.role IN ('owner', 'admin') AND EXISTS (SELECT 1 FROM project p WHERE p.id = target)
        UNION ALL
        SELECT perm_grant_role(m.role, g.role) FROM project_grant g
        JOIN app_user m ON m.id = actor AND m.role IS NOT NULL
        WHERE g.project_id = target AND perm_grant_reaches(actor, m.role, g.subject_type, g.user_id, g.group_id)
    ) roles
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.app_user_left() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    DELETE FROM api_token WHERE user_id = NEW.id;
    DELETE FROM armature_token WHERE user_id = NEW.id;
    DELETE FROM stator_token WHERE user_id = NEW.id;
    DELETE FROM project_grant WHERE user_id = NEW.id;
    UPDATE requirement SET owner_id = NULL WHERE owner_id = NEW.id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TABLE public.oidc_group_role (
    id uuid DEFAULT uuidv7() NOT NULL,
    group_ref text NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT oidc_group_role_ref_not_blank CHECK ((btrim(group_ref) <> ''::text)),
    CONSTRAINT oidc_group_role_role_check CHECK ((role = ANY (ARRAY['admin'::text, 'member'::text])))
);
ALTER TABLE ONLY public.oidc_group_role
    ADD CONSTRAINT oidc_group_role_pkey PRIMARY KEY (id);
CREATE UNIQUE INDEX oidc_group_role_ref_idx ON public.oidc_group_role USING btree (group_ref);

-- A provider group granted something across the installation maps to admin where it administers, else to member.
INSERT INTO public.oidc_group_role (group_ref, role)
SELECT g.external_ref, CASE WHEN bool_or('administerInstallation' = ANY (r.permissions)) THEN 'admin' ELSE 'member' END
FROM role_assignment a
JOIN role r ON r.key = a.role
JOIN groups g ON g.id = a.group_id
WHERE a.project_id IS NULL AND g.external_ref IS NOT NULL AND EXISTS (SELECT 1 FROM oidc_provider)
GROUP BY g.external_ref;

CREATE TRIGGER oidc_group_role_needs_a_provider BEFORE INSERT ON public.oidc_group_role FOR EACH ROW EXECUTE FUNCTION public.needs_its_one_row('oidc_provider');
CREATE TRIGGER oidc_group_role_set_updated_at BEFORE UPDATE ON public.oidc_group_role FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();
ALTER TABLE ONLY public.oidc_group_role FORCE ROW LEVEL SECURITY;
ALTER TABLE public.oidc_group_role ENABLE ROW LEVEL SECURITY;
CREATE POLICY oidc_group_role_admins_only ON public.oidc_group_role AS RESTRICTIVE TO exciter_app USING (public.current_actor_administers()) WITH CHECK (public.current_actor_administers());
CREATE POLICY oidc_group_role_installation ON public.oidc_group_role USING (true) WITH CHECK (true);
GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.oidc_group_role TO exciter_app;
GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.oidc_group_role TO exciter_admin;

-- +goose StatementBegin
CREATE FUNCTION public.oidc_provider_forgets_group_roles() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
    AS $$
BEGIN
    DELETE FROM oidc_group_role;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER oidc_provider_forgets_group_roles AFTER DELETE ON public.oidc_provider FOR EACH ROW EXECUTE FUNCTION public.oidc_provider_forgets_group_roles();

DROP TRIGGER installation_keeps_administrator ON role_assignment;
DROP TRIGGER installation_keeps_administrator ON role;
DROP TRIGGER installation_keeps_administrator ON group_member;
DROP FUNCTION installation_keeps_administrator();
DROP FUNCTION perm_active_administrators();
DROP FUNCTION perm_grants_reaching(uuid, uuid);
DROP TRIGGER group_member_keeps_everyone ON group_member;
DROP FUNCTION group_member_keeps_everyone();
DROP TRIGGER group_member_everyone_members ON group_member;
DROP FUNCTION group_member_everyone_members();
DROP FUNCTION app_user_joins_everyone();
DROP TRIGGER groups_builtin_guard ON groups;
DROP FUNCTION groups_builtin_guard();
DROP TABLE role_assignment;
DROP FUNCTION role_assignment_guard();
DROP TABLE role;
DROP FUNCTION role_guard();
DROP FUNCTION current_actor_creates_projects();
DROP FUNCTION perm_is_administrator(uuid);
DROP FUNCTION perm_installation_grants(uuid);
DROP FUNCTION perm_role_held(text, text[]);
DELETE FROM groups WHERE builtin IS NOT NULL;
UPDATE groups SET name = left(name, length(name) - length(' (made by hand)'))
WHERE name LIKE '% (made by hand)' AND lower(left(name, length(name) - length(' (made by hand)'))) = 'everyone';
DROP INDEX groups_builtin_idx;
ALTER TABLE groups DROP COLUMN builtin;

ALTER TABLE project FORCE ROW LEVEL SECURITY;
ALTER TABLE group_member FORCE ROW LEVEL SECURITY;
ALTER TABLE groups FORCE ROW LEVEL SECURITY;
ALTER TABLE app_user FORCE ROW LEVEL SECURITY;
