-- +goose Up
-- What is left of the organization is names (#108), as Armature's #218 found: its row is the installation, a
-- membership is a role on the account, the identity provider and the Armature connection are one row each, and no
-- table carries org_id. The permission policies keep every wall; only what named the organization changes.

-- A function's body is not tracked, so one that names what goes would break only when it runs. Those this upgrade
-- rewrites or drops are the only ones that may.
-- +goose StatementBegin
DO $$
DECLARE
    listed CONSTANT int := 10;
    total int;
    names text;
BEGIN
    SELECT count(*) INTO total FROM org;
    IF total > 1 THEN
        RAISE EXCEPTION 'This database holds % organizations, and from this release the tables name none, so a row could not say which it belongs to. Delete the extra ones, then upgrade again.', total
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT count(*), string_agg(proname, ', ' ORDER BY proname) FILTER (WHERE n <= listed) INTO total, names
    FROM (SELECT proname, row_number() OVER (ORDER BY proname) AS n
          FROM pg_proc
          WHERE pronamespace = 'public'::regnamespace
            AND (prosrc ~ '\m(org_member|org_join_request|org_id|current_org_id|installation_org_id)\M'
                 OR prosrc ~* '\m(from|join|into|update|table)\s+org\M')
            AND proname NOT IN ('administered_without', 'app_user_keeps_an_administrator', 'armature_endpoint',
                                'current_actor_administers', 'current_org_id', 'group_member_scope_guard',
                                'installation_org_id', 'link_type_seed', 'org_member_keeps_an_administrator',
                                'perm_project_holds', 'perm_project_roles', 'project_grant_defaults',
                                'project_grant_guest_check', 'session_keeps_its_opening', 'theme_unshared_default')) named;
    IF total > 0 THEN
        IF total > listed THEN
            names := names || format(' and %s more', total - listed);
        END IF;
        RAISE EXCEPTION 'The organization''s tables and columns go with this release, and these functions read them: %. Change or drop them so they no longer name the organization, then upgrade again.', names
            USING ERRCODE = 'dependent_objects_still_exist';
    END IF;
END $$;
-- +goose StatementEnd

-- A membership becomes the account's role; NULL is somebody who asked to join and was not let in yet.
ALTER TABLE app_user
    ADD COLUMN role text CONSTRAINT app_user_role_check CHECK (role IN ('owner', 'admin', 'member', 'guest')),
    ADD COLUMN role_source text NOT NULL DEFAULT 'manual'
        CONSTRAINT app_user_role_source_check CHECK (role_source IN ('manual', 'oidc')),
    ADD COLUMN joined_at timestamptz,
    ADD CONSTRAINT app_user_owner_is_manual CHECK (role IS DISTINCT FROM 'owner' OR role_source = 'manual'),
    ADD CONSTRAINT app_user_joined_with_a_role CHECK ((role IS NULL) = (joined_at IS NULL));
-- The role is not an edit of the account, so its updated_at stays.
ALTER TABLE app_user DISABLE TRIGGER app_user_set_updated_at;
UPDATE app_user u SET role = m.org_role, role_source = m.role_source, joined_at = m.created_at
FROM org_member m WHERE m.user_id = u.id;
ALTER TABLE app_user ENABLE TRIGGER app_user_set_updated_at;

-- What only a member could hold names the account now, and a trigger keeps it to members.
ALTER TABLE api_token DROP CONSTRAINT api_token_user_id_org_member_fkey;
ALTER TABLE armature_token DROP CONSTRAINT armature_token_user_id_fkey,
    ADD CONSTRAINT armature_token_user_id_fkey FOREIGN KEY (user_id) REFERENCES app_user (id) ON DELETE CASCADE;
ALTER TABLE project_grant DROP CONSTRAINT project_grant_user_id_fkey,
    ADD CONSTRAINT project_grant_user_id_fkey FOREIGN KEY (user_id) REFERENCES app_user (id) ON DELETE CASCADE;
ALTER TABLE requirement DROP CONSTRAINT requirement_owner_id_fkey,
    ADD CONSTRAINT requirement_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES app_user (id) ON DELETE SET NULL;

ALTER TABLE org RENAME TO installation;
ALTER TABLE installation DROP COLUMN slug, DROP COLUMN settings, DROP COLUMN archived_at;
ALTER TABLE org_join_request RENAME TO join_request;
ALTER TABLE oidc_group_role RENAME COLUMN org_role TO role;
ALTER TABLE oidc_group_role RENAME CONSTRAINT oidc_group_role_org_role_check TO oidc_group_role_role_check;
ALTER TABLE oidc_group_role RENAME CONSTRAINT oidc_group_role_org_role_not_null TO oidc_group_role_role_not_null;

-- A table's rename leaves its constraints, indexes, triggers and policies named for the old one, the not-null
-- constraints Postgres 18 names among them.
-- +goose StatementBegin
DO $$
DECLARE
    t record;
    o record;
BEGIN
    FOR t IN SELECT * FROM (VALUES ('installation', 'org_', 'installation_'), ('join_request', 'org_join_request_', 'join_request_'))
                          AS r (tab, old, new)
    LOOP
        FOR o IN SELECT conname FROM pg_constraint WHERE conrelid = t.tab::regclass AND starts_with(conname, t.old) LOOP
            EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', t.tab, o.conname, t.new || substr(o.conname, length(t.old) + 1));
        END LOOP;
        FOR o IN SELECT c.relname FROM pg_index x JOIN pg_class c ON c.oid = x.indexrelid
                 WHERE x.indrelid = t.tab::regclass AND starts_with(c.relname, t.old) LOOP
            EXECUTE format('ALTER INDEX %I RENAME TO %I', o.relname, t.new || substr(o.relname, length(t.old) + 1));
        END LOOP;
        FOR o IN SELECT tgname FROM pg_trigger WHERE tgrelid = t.tab::regclass AND NOT tgisinternal AND starts_with(tgname, t.old) LOOP
            EXECUTE format('ALTER TRIGGER %I ON %I RENAME TO %I', o.tgname, t.tab, t.new || substr(o.tgname, length(t.old) + 1));
        END LOOP;
        FOR o IN SELECT policyname FROM pg_policies WHERE schemaname = 'public' AND tablename = t.tab AND starts_with(policyname, t.old) LOOP
            EXECUTE format('ALTER POLICY %I ON %I RENAME TO %I', o.policyname, t.tab, t.new || substr(o.policyname, length(t.old) + 1));
        END LOOP;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER FUNCTION org_default_theme_guard() RENAME TO installation_default_theme_guard;
ALTER FUNCTION org_link_type_seed() RENAME TO installation_link_type_seed;

-- Nothing names the row any more, so nothing could tell a second one apart; the same holds for the provider and
-- the connection.
CREATE UNIQUE INDEX installation_one_row_idx ON installation ((true));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION administered_without(leaving uuid) RETURNS boolean
    LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('administered_without'));
    RETURN EXISTS (SELECT 1 FROM app_user
                   WHERE id <> leaving AND role IN ('owner', 'admin') AND is_active);
END;
$$;

-- The last active administrator stays one and switched on, whichever way somebody tries.
CREATE OR REPLACE FUNCTION app_user_keeps_an_administrator() RETURNS trigger
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

-- The app role takes no membership away, as it could not delete one, and makes and unmakes no owner; the provider
-- unmakes none, and a role is dated from when it was given.
CREATE FUNCTION app_user_role_guard() RETURNS trigger
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

-- A role taken away takes what only a member holds, as deleting the membership did through its keys.
CREATE FUNCTION app_user_left() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM api_token WHERE user_id = NEW.id;
    DELETE FROM armature_token WHERE user_id = NEW.id;
    DELETE FROM project_grant WHERE user_id = NEW.id;
    UPDATE requirement SET owner_id = NULL WHERE owner_id = NEW.id;
    RETURN NULL;
END;
$$;

-- held_by_a_member keeps the column named by the first argument to members, as a key to the membership did.
CREATE FUNCTION held_by_a_member() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    holder uuid := (to_jsonb(NEW) ->> TG_ARGV[0])::uuid;
BEGIN
    IF holder IS NOT NULL AND NOT EXISTS (SELECT 1 FROM app_user WHERE id = holder AND role IS NOT NULL) THEN
        RAISE EXCEPTION 'only a member here can hold that' USING ERRCODE = 'foreign_key_violation',
            CONSTRAINT = TG_TABLE_NAME || '_' || TG_ARGV[0] || '_member';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION current_actor_administers() RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
    SELECT perm_token_whole(current_actor_id()) AND EXISTS (
        SELECT 1 FROM app_user
        WHERE id = current_actor_id() AND role IN ('owner', 'admin'))
$$;

CREATE OR REPLACE FUNCTION armature_endpoint() RETURNS TABLE(base_url text, armature_org_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
    SELECT c.base_url, c.armature_org_id
    FROM armature_connection c
    WHERE EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.role IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION group_member_scope_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM app_user WHERE id = NEW.user_id AND role IS NOT NULL) THEN
        RAISE EXCEPTION 'only a member here can join a group' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION perm_project_holds(project uuid, actor uuid, permission text) RETURNS boolean
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

CREATE OR REPLACE FUNCTION perm_project_roles(target uuid, actor uuid) RETURNS text[]
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

CREATE OR REPLACE FUNCTION project_grant_defaults() RETURNS trigger
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

CREATE OR REPLACE FUNCTION project_grant_guest_check() RETURNS trigger
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

CREATE OR REPLACE FUNCTION theme_unshared_default() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE installation SET default_theme_id = NULL WHERE default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION installation_default_theme_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.default_theme_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM theme WHERE id = NEW.default_theme_id AND shared
    ) THEN
        RAISE EXCEPTION 'the installation''s default theme has to be a shared theme'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION link_type_project_reach() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM link_type t WHERE t.id = NEW.link_type_id
                   AND (t.project_id IS NULL OR t.project_id = NEW.project_id)) THEN
        RAISE EXCEPTION 'a project sets the link types for all projects and its own'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_project_reach';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION requirement_number() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
BEGIN
    UPDATE project SET next_number = next_number + 1
    WHERE id = NEW.project_id
    RETURNING next_number - 1 INTO NEW.number;
    IF NEW.number IS NULL THEN
        RAISE EXCEPTION 'a requirement needs a project'
            USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'requirement_project_fkey';
    END IF;
    IF NEW.rank IS NULL THEN
        NEW.rank := lpad(to_hex(NEW.number), 10, '0') || 'V';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION session_keeps_its_opening() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.proof IS DISTINCT FROM OLD.proof THEN
        RAISE EXCEPTION 'a session keeps the proof it was opened with' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

-- Removing the provider takes its group roles, and removing the connection the members' tokens, as the keys to
-- their organization's row did.
CREATE FUNCTION oidc_provider_forgets_group_roles() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
BEGIN
    DELETE FROM oidc_group_role;
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION armature_connection_forgets_links() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM armature_remote_link;
    END IF;
    IF TG_OP = 'DELETE' THEN
        DELETE FROM armature_token;
    END IF;
    RETURN NULL;
END;
$$;

-- A group role needs a provider and a token a connection, as their keys to those rows asked.
CREATE FUNCTION needs_its_one_row() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
DECLARE
    present boolean;
BEGIN
    EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I)', TG_ARGV[0]) INTO present;
    IF NOT present THEN
        RAISE EXCEPTION 'there is no % to hold this yet', replace(TG_ARGV[0], '_', ' ') USING ERRCODE = 'foreign_key_violation',
            CONSTRAINT = TG_TABLE_NAME || '_needs_' || TG_ARGV[0];
    END IF;
    RETURN NEW;
END;
$$;

-- An attachment's key keeps the installation's prefix, under which every earlier file lives.
CREATE FUNCTION attachment_object_key() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW.object_key := OLD.object_key;
    ELSE
        NEW.object_key := 'org/' || (SELECT id FROM installation) || '/requirement/' || NEW.requirement_id || '/' || NEW.id;
    END IF;
    RETURN NEW;
END;
$$;

-- A tombstone may name only a file of the installation's.
CREATE FUNCTION attachment_tombstone_own_prefix() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT starts_with(NEW.object_key, 'org/' || (SELECT id FROM installation) || '/') THEN
        RAISE EXCEPTION 'a tombstone names a file of this installation' USING ERRCODE = 'check_violation',
            CONSTRAINT = 'attachment_tombstone_own_prefix';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION link_type_seed() RETURNS void
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
    INSERT INTO link_type (key, name, forward, backward, acyclic, suspect_end, counts) VALUES
        ('derives', 'Derives', 'derives from', 'is derived into', true, 'target', 'decomposition'),
        ('refines', 'Refines', 'refines', 'is refined by', true, 'target', 'decomposition'),
        ('satisfies', 'Satisfies', 'satisfies', 'is satisfied by', true, 'target', 'decomposition'),
        ('verifies', 'Verifies', 'verifies', 'is verified by', true, 'target', 'verification'),
        ('conflicts', 'Conflicts', 'conflicts with', 'conflicts with', false, 'both', 'nothing'),
        ('relates', 'Relates', 'relates to', 'relates to', false, 'none', 'nothing')
$$;

CREATE OR REPLACE FUNCTION installation_link_type_seed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'public', 'pg_temp'
AS $$
BEGIN
    PERFORM link_type_seed();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
DROP FUNCTION link_type_seed(uuid);

DROP TRIGGER app_user_keeps_an_administrator ON app_user;
CREATE TRIGGER app_user_keeps_an_administrator BEFORE DELETE OR UPDATE OF is_active, role ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_keeps_an_administrator();
CREATE TRIGGER app_user_role_guard BEFORE INSERT OR UPDATE OF role, role_source ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_role_guard();
CREATE TRIGGER app_user_left AFTER UPDATE OF role ON app_user
    FOR EACH ROW WHEN (OLD.role IS NOT NULL AND NEW.role IS NULL) EXECUTE FUNCTION app_user_left();
CREATE TRIGGER api_token_held_by_a_member BEFORE INSERT OR UPDATE OF user_id ON api_token
    FOR EACH ROW EXECUTE FUNCTION held_by_a_member('user_id');
CREATE TRIGGER armature_token_held_by_a_member BEFORE INSERT OR UPDATE OF user_id ON armature_token
    FOR EACH ROW EXECUTE FUNCTION held_by_a_member('user_id');
CREATE TRIGGER project_grant_held_by_a_member BEFORE INSERT OR UPDATE OF user_id ON project_grant
    FOR EACH ROW EXECUTE FUNCTION held_by_a_member('user_id');
CREATE TRIGGER requirement_owned_by_a_member BEFORE INSERT OR UPDATE OF owner_id ON requirement
    FOR EACH ROW EXECUTE FUNCTION held_by_a_member('owner_id');
CREATE TRIGGER oidc_provider_forgets_group_roles AFTER DELETE ON oidc_provider
    FOR EACH ROW EXECUTE FUNCTION oidc_provider_forgets_group_roles();
CREATE TRIGGER oidc_group_role_needs_a_provider BEFORE INSERT ON oidc_group_role
    FOR EACH ROW EXECUTE FUNCTION needs_its_one_row('oidc_provider');
CREATE TRIGGER armature_token_needs_a_connection BEFORE INSERT ON armature_token
    FOR EACH ROW EXECUTE FUNCTION needs_its_one_row('armature_connection');

-- Who the app role sees and who it may let in: the people with a role, and only an administrator adds one, never
-- an owner, as the membership's own policies said.
ALTER POLICY app_user_installation ON app_user USING (role IS NOT NULL) WITH CHECK (role IS NOT NULL);
CREATE POLICY app_user_admins_add ON app_user AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (current_actor_administers() AND role IS DISTINCT FROM 'owner');
ALTER POLICY audit_log_admins_read ON audit_log
    USING (EXISTS (SELECT 1 FROM app_user WHERE id = current_actor_id() AND role IN ('owner', 'admin')));
-- Deleting an account took its membership with it, and any member could; nobody leaves that way, an administrator
-- switches them off.
REVOKE DELETE ON app_user FROM exciter_app;

DROP TABLE org_member;
DROP FUNCTION org_member_keeps_an_administrator();
DROP FUNCTION org_member_owner_keeps_standing();
DROP FUNCTION org_member_owner_not_by_provider();

-- The keys to the provider's and the connection's rows go before the rows' own, and what reads a column before it.
ALTER TABLE oidc_group_role DROP CONSTRAINT oidc_group_role_org_id_fkey;
ALTER TABLE armature_token DROP CONSTRAINT armature_token_org_id_fkey;
ALTER TABLE attachment ALTER COLUMN object_key DROP EXPRESSION;
CREATE TRIGGER attachment_object_key BEFORE INSERT OR UPDATE OF object_key ON attachment
    FOR EACH ROW EXECUTE FUNCTION attachment_object_key();
ALTER TABLE attachment_tombstone DROP CONSTRAINT attachment_tombstone_own_prefix;
CREATE TRIGGER attachment_tombstone_own_prefix BEFORE INSERT OR UPDATE OF object_key ON attachment_tombstone
    FOR EACH ROW EXECUTE FUNCTION attachment_tombstone_own_prefix();
DROP TRIGGER session_keeps_its_opening ON user_session;
CREATE TRIGGER session_keeps_its_opening BEFORE UPDATE OF proof ON user_session
    FOR EACH ROW EXECUTE FUNCTION session_keeps_its_opening();
ALTER TABLE user_session DROP COLUMN current_org_id;

-- Without CASCADE, so anything the check above did not foresee stops the drop.
-- +goose StatementBegin
DO $$
DECLARE
    t record;
BEGIN
    FOR t IN
        SELECT c.relname
        FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
        WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
          AND a.attname = 'org_id' AND NOT a.attisdropped
        ORDER BY c.relname
    LOOP
        EXECUTE format('ALTER TABLE %I DROP COLUMN org_id', t.relname);
    END LOOP;
END $$;
-- +goose StatementEnd

CREATE UNIQUE INDEX oidc_provider_one_row_idx ON oidc_provider ((true));
CREATE UNIQUE INDEX armature_connection_one_row_idx ON armature_connection ((true));

DROP FUNCTION current_org_id();
DROP FUNCTION installation_org_id();

-- +goose Down
-- The way back gives every role its membership row again, dated when the role was given; when a membership last
-- changed was not kept. The installation's slug is the one a first start gives it, and its settings are empty.

DROP INDEX armature_connection_one_row_idx;
DROP INDEX oidc_provider_one_row_idx;
DROP INDEX installation_one_row_idx;

ALTER TABLE installation RENAME TO org;
ALTER TABLE join_request RENAME TO org_join_request;
ALTER TABLE oidc_group_role RENAME CONSTRAINT oidc_group_role_role_not_null TO oidc_group_role_org_role_not_null;
ALTER TABLE oidc_group_role RENAME CONSTRAINT oidc_group_role_role_check TO oidc_group_role_org_role_check;
ALTER TABLE oidc_group_role RENAME COLUMN role TO org_role;

-- +goose StatementBegin
DO $$
DECLARE
    t record;
    o record;
BEGIN
    FOR t IN SELECT * FROM (VALUES ('org', 'installation_', 'org_'), ('org_join_request', 'join_request_', 'org_join_request_'))
                          AS r (tab, old, new)
    LOOP
        FOR o IN SELECT conname FROM pg_constraint WHERE conrelid = t.tab::regclass AND starts_with(conname, t.old) LOOP
            EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', t.tab, o.conname, t.new || substr(o.conname, length(t.old) + 1));
        END LOOP;
        FOR o IN SELECT c.relname FROM pg_index x JOIN pg_class c ON c.oid = x.indexrelid
                 WHERE x.indrelid = t.tab::regclass AND starts_with(c.relname, t.old) LOOP
            EXECUTE format('ALTER INDEX %I RENAME TO %I', o.relname, t.new || substr(o.relname, length(t.old) + 1));
        END LOOP;
        FOR o IN SELECT tgname FROM pg_trigger WHERE tgrelid = t.tab::regclass AND NOT tgisinternal AND starts_with(tgname, t.old) LOOP
            EXECUTE format('ALTER TRIGGER %I ON %I RENAME TO %I', o.tgname, t.tab, t.new || substr(o.tgname, length(t.old) + 1));
        END LOOP;
        FOR o IN SELECT policyname FROM pg_policies WHERE schemaname = 'public' AND tablename = t.tab AND starts_with(policyname, t.old) LOOP
            EXECUTE format('ALTER POLICY %I ON %I RENAME TO %I', o.policyname, t.tab, t.new || substr(o.policyname, length(t.old) + 1));
        END LOOP;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE org ADD COLUMN slug citext, ADD COLUMN settings jsonb NOT NULL DEFAULT '{}'::jsonb, ADD COLUMN archived_at timestamptz;
UPDATE org SET slug = 'exciter';
ALTER TABLE org ALTER COLUMN slug SET NOT NULL,
    ADD CONSTRAINT org_slug_key UNIQUE (slug),
    ADD CONSTRAINT org_slug_shape CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$'::citext);

ALTER FUNCTION installation_default_theme_guard() RENAME TO org_default_theme_guard;
ALTER FUNCTION installation_link_type_seed() RENAME TO org_link_type_seed;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.current_org_id()
 RETURNS uuid
 LANGUAGE sql
 STABLE PARALLEL SAFE
AS $function$
    SELECT (SELECT id FROM org)
$function$;

CREATE OR REPLACE FUNCTION public.installation_org_id()
 RETURNS uuid
 LANGUAGE plpgsql
 STABLE
AS $function$
DECLARE
    found uuid[];
BEGIN
    SELECT array_agg(id) INTO found FROM org;
    IF cardinality(found) = 1 THEN
        RETURN found[1];
    END IF;
    IF found IS NULL THEN
        RAISE EXCEPTION 'This database has no installation yet. Start Exciter once, so its first start makes it, then try again.'
            USING ERRCODE = 'not_null_violation';
    END IF;
    RAISE EXCEPTION 'This database holds % organizations, and Exciter runs one per installation, so a row cannot tell which it belongs to. Delete the extra ones, then try again.', cardinality(found)
        USING ERRCODE = 'cardinality_violation';
END;
$function$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION installation_org_id() TO exciter_app, exciter_admin;

-- Every table gets its column back, filled with the installation's id; the default is found once per table.
-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['api_token', 'api_token_project', 'armature_connection', 'armature_issue_link', 'armature_remote_link', 'armature_token', 'attachment', 'attachment_tombstone', 'attribute_def', 'attribute_def_type', 'audit_log', 'group_member', 'groups', 'import_job', 'link_type', 'link_type_project', 'link_type_rule', 'oidc_group_role', 'oidc_login', 'oidc_provider', 'org_join_request', 'outbox_event', 'project', 'project_grant', 'project_key_alias', 'req_status', 'requirement', 'requirement_change', 'requirement_label', 'requirement_old_key', 'requirement_revision', 'requirement_type', 'saved_view', 'theme', 'theme_asset', 'trace_link', 'transition_rule', 'user_theme', 'workflow', 'workflow_step', 'workflow_transition'] LOOP
        EXECUTE format('ALTER TABLE %I ADD COLUMN org_id uuid NOT NULL DEFAULT installation_org_id()', t);
    END LOOP;
END $$;
-- +goose StatementEnd
ALTER TABLE groups ADD CONSTRAINT groups_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE group_member ADD CONSTRAINT group_member_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE oidc_provider ADD CONSTRAINT oidc_provider_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE oidc_login ADD CONSTRAINT oidc_login_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE org_join_request ADD CONSTRAINT org_join_request_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE outbox_event ADD CONSTRAINT outbox_event_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE theme ADD CONSTRAINT theme_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE theme_asset ADD CONSTRAINT theme_asset_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE user_theme ADD CONSTRAINT user_theme_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE project ADD CONSTRAINT project_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE project_key_alias ADD CONSTRAINT project_key_alias_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE project_grant ADD CONSTRAINT project_grant_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE requirement ADD CONSTRAINT requirement_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE requirement_old_key ADD CONSTRAINT requirement_old_key_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE requirement_type ADD CONSTRAINT requirement_type_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE attribute_def ADD CONSTRAINT attribute_def_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE attribute_def_type ADD CONSTRAINT attribute_def_type_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE req_status ADD CONSTRAINT req_status_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE workflow ADD CONSTRAINT workflow_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE link_type ADD CONSTRAINT link_type_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE link_type_project ADD CONSTRAINT link_type_project_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE link_type_rule ADD CONSTRAINT link_type_rule_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE trace_link ADD CONSTRAINT trace_link_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE attachment ADD CONSTRAINT attachment_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE api_token ADD CONSTRAINT api_token_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE import_job ADD CONSTRAINT import_job_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE armature_connection ADD CONSTRAINT armature_connection_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE armature_remote_link ADD CONSTRAINT armature_remote_link_org_id_fkey FOREIGN KEY (org_id) REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE oidc_provider ADD CONSTRAINT oidc_provider_pkey PRIMARY KEY (org_id);
ALTER TABLE armature_connection ADD CONSTRAINT armature_connection_pkey PRIMARY KEY (org_id);
GRANT INSERT (org_id) ON armature_connection TO exciter_app;
ALTER TABLE oidc_group_role ADD CONSTRAINT oidc_group_role_org_id_fkey FOREIGN KEY (org_id) REFERENCES oidc_provider (org_id) ON DELETE CASCADE;
ALTER TABLE armature_token ADD CONSTRAINT armature_token_org_id_fkey FOREIGN KEY (org_id) REFERENCES armature_connection (org_id) ON DELETE CASCADE;

DROP TRIGGER attachment_tombstone_own_prefix ON attachment_tombstone;
DROP FUNCTION attachment_tombstone_own_prefix();
ALTER TABLE attachment_tombstone ADD CONSTRAINT attachment_tombstone_own_prefix
    CHECK (starts_with(object_key, 'org/' || org_id::text || '/'));
DROP TRIGGER attachment_object_key ON attachment;
DROP FUNCTION attachment_object_key();
ALTER TABLE attachment DROP COLUMN object_key;
ALTER TABLE attachment ADD COLUMN object_key text
        GENERATED ALWAYS AS ('org/' || org_id::text || '/requirement/' || requirement_id::text || '/' || id::text) STORED,
    ADD CONSTRAINT attachment_object_key_unique UNIQUE (object_key);

ALTER TABLE user_session ADD COLUMN current_org_id uuid REFERENCES org (id) ON DELETE SET NULL;
DROP TRIGGER session_keeps_its_opening ON user_session;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.session_keeps_its_opening()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF NEW.proof IS DISTINCT FROM OLD.proof THEN
        RAISE EXCEPTION 'a session keeps the proof it was opened with' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.current_org_id IS NOT NULL AND NEW.current_org_id IS DISTINCT FROM OLD.current_org_id THEN
        RAISE EXCEPTION 'a session keeps the organization it was opened in' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd
CREATE TRIGGER session_keeps_its_opening BEFORE UPDATE OF current_org_id, proof ON user_session
    FOR EACH ROW EXECUTE FUNCTION session_keeps_its_opening();

CREATE TABLE org_member (
    org_id uuid NOT NULL DEFAULT installation_org_id() REFERENCES org (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    org_role text NOT NULL CONSTRAINT org_member_org_role_check CHECK (org_role IN ('owner', 'admin', 'member', 'guest')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    role_source text NOT NULL DEFAULT 'manual' CONSTRAINT org_member_role_source_check CHECK (role_source IN ('manual', 'oidc')),
    CONSTRAINT org_member_pkey PRIMARY KEY (user_id),
    CONSTRAINT org_member_owner_is_manual CHECK (org_role <> 'owner' OR role_source = 'manual')
);
CREATE INDEX org_member_user_idx ON org_member (user_id);
INSERT INTO org_member (user_id, org_role, role_source, created_at, updated_at)
SELECT id, role, role_source, joined_at, joined_at FROM app_user WHERE role IS NOT NULL;
ALTER TABLE org_member ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_member FORCE ROW LEVEL SECURITY;
REVOKE DELETE ON org_member FROM exciter_app;
CREATE POLICY org_member_installation ON org_member USING (true) WITH CHECK (true);
CREATE POLICY org_member_admins_add ON org_member AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (current_actor_administers() AND org_role <> 'owner');
CREATE POLICY org_member_admins_change ON org_member AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers() AND org_role <> 'owner');

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_member_keeps_an_administrator()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF OLD.org_role IN ('owner', 'admin') AND (TG_OP = 'DELETE' OR NEW.org_role NOT IN ('owner', 'admin'))
       AND EXISTS (SELECT 1 FROM org)
       AND EXISTS (SELECT 1 FROM app_user WHERE id = OLD.user_id AND is_active)
       AND NOT administered_without(OLD.user_id) THEN
        RAISE EXCEPTION 'the installation keeps its last active administrator' USING ERRCODE = 'check_violation';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$function$;

CREATE OR REPLACE FUNCTION public.org_member_owner_keeps_standing()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF current_user = 'exciter_app' AND OLD.org_role = 'owner' AND NEW.org_role <> 'owner' THEN
        RAISE EXCEPTION 'an owner''s role is not changed here' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.org_member_owner_not_by_provider()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF OLD.org_role = 'owner' AND NEW.org_role <> 'owner' AND NEW.role_source = 'oidc' THEN
        RAISE EXCEPTION 'the identity provider cannot change the role of an organization''s owner'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd
CREATE TRIGGER org_member_keeps_an_administrator BEFORE DELETE OR UPDATE OF org_role ON org_member
    FOR EACH ROW EXECUTE FUNCTION org_member_keeps_an_administrator();
CREATE TRIGGER org_member_owner_keeps_standing BEFORE UPDATE OF org_role ON org_member
    FOR EACH ROW EXECUTE FUNCTION org_member_owner_keeps_standing();
CREATE TRIGGER org_member_owner_not_by_provider BEFORE UPDATE OF org_role, role_source ON org_member
    FOR EACH ROW EXECUTE FUNCTION org_member_owner_not_by_provider();
CREATE TRIGGER org_member_set_updated_at BEFORE UPDATE ON org_member
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER api_token_held_by_a_member ON api_token;
DROP TRIGGER armature_token_held_by_a_member ON armature_token;
DROP TRIGGER project_grant_held_by_a_member ON project_grant;
DROP TRIGGER requirement_owned_by_a_member ON requirement;
DROP FUNCTION held_by_a_member();
DROP TRIGGER oidc_group_role_needs_a_provider ON oidc_group_role;
DROP TRIGGER armature_token_needs_a_connection ON armature_token;
DROP FUNCTION needs_its_one_row();
DROP TRIGGER oidc_provider_forgets_group_roles ON oidc_provider;
DROP FUNCTION oidc_provider_forgets_group_roles();
ALTER TABLE api_token ADD CONSTRAINT api_token_user_id_org_member_fkey FOREIGN KEY (user_id) REFERENCES org_member (user_id) ON DELETE CASCADE;
ALTER TABLE armature_token DROP CONSTRAINT armature_token_user_id_fkey,
    ADD CONSTRAINT armature_token_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member (user_id) ON DELETE CASCADE;
ALTER TABLE project_grant DROP CONSTRAINT project_grant_user_id_fkey,
    ADD CONSTRAINT project_grant_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member (user_id) ON DELETE CASCADE;
ALTER TABLE requirement DROP CONSTRAINT requirement_owner_id_fkey,
    ADD CONSTRAINT requirement_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES org_member (user_id) ON DELETE SET NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.link_type_seed(org uuid)
 RETURNS void
 LANGUAGE sql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    INSERT INTO link_type (org_id, key, name, forward, backward, acyclic, suspect_end, counts) VALUES
        (org, 'derives', 'Derives', 'derives from', 'is derived into', true, 'target', 'decomposition'),
        (org, 'refines', 'Refines', 'refines', 'is refined by', true, 'target', 'decomposition'),
        (org, 'satisfies', 'Satisfies', 'satisfies', 'is satisfied by', true, 'target', 'decomposition'),
        (org, 'verifies', 'Verifies', 'verifies', 'is verified by', true, 'target', 'verification'),
        (org, 'conflicts', 'Conflicts', 'conflicts with', 'conflicts with', false, 'both', 'nothing'),
        (org, 'relates', 'Relates', 'relates to', 'relates to', false, 'none', 'nothing')
$function$;

CREATE OR REPLACE FUNCTION public.org_link_type_seed()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    PERFORM link_type_seed(NEW.id);
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.administered_without(leaving uuid)
 RETURNS boolean
 LANGUAGE plpgsql
AS $function$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('administered_without'));
    RETURN EXISTS (SELECT 1 FROM org_member m JOIN app_user u ON u.id = m.user_id
                   WHERE m.user_id <> leaving AND m.org_role IN ('owner', 'admin') AND u.is_active);
END;
$function$;

CREATE OR REPLACE FUNCTION public.app_user_keeps_an_administrator()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF OLD.is_active AND NOT NEW.is_active AND EXISTS (
        SELECT 1 FROM org_member m
        WHERE m.user_id = NEW.id AND m.org_role IN ('owner', 'admin')
          AND NOT administered_without(NEW.id)) THEN
        RAISE EXCEPTION 'the last active administrator cannot be switched off' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.armature_endpoint()
 RETURNS TABLE(base_url text, armature_org_id uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT c.base_url, c.armature_org_id
    FROM armature_connection c
    WHERE EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = current_actor_id())
$function$;

CREATE OR REPLACE FUNCTION public.current_actor_administers()
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_token_whole(current_actor_id()) AND EXISTS (
        SELECT 1 FROM org_member
        WHERE user_id = current_actor_id() AND org_role IN ('owner', 'admin'))
$function$;

CREATE OR REPLACE FUNCTION public.group_member_scope_guard()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM org_member WHERE user_id = NEW.user_id) THEN
        RAISE EXCEPTION 'only a member here can join a group' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.perm_project_holds(project uuid, actor uuid, permission text)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_token_reaches(actor, perm_project_holds.project) AND EXISTS (
        SELECT 1 FROM project p
        JOIN org_member m ON m.user_id = actor
        WHERE p.id = perm_project_holds.project AND (p.archived_at IS NULL OR permission IN ('view', 'administer'))
          AND (m.org_role IN ('owner', 'admin') OR EXISTS (
              SELECT 1 FROM project_grant g
              WHERE g.project_id = p.id
                AND perm_grant_reaches(actor, m.org_role, g.subject_type, g.user_id, g.group_id)
                AND permission = ANY (perm_role_rights(perm_grant_role(m.org_role, g.role))))))
$function$;

CREATE OR REPLACE FUNCTION public.perm_project_roles(target uuid, actor uuid)
 RETURNS text[]
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT COALESCE(array_agg(DISTINCT role), '{}') FROM (
        SELECT 'admin' AS role FROM org_member m
        WHERE m.user_id = actor AND m.org_role IN ('owner', 'admin') AND EXISTS (SELECT 1 FROM project p WHERE p.id = target)
        UNION ALL
        SELECT perm_grant_role(m.org_role, g.role) FROM project_grant g
        JOIN org_member m ON m.user_id = actor
        WHERE g.project_id = target AND perm_grant_reaches(actor, m.org_role, g.subject_type, g.user_id, g.group_id)
    ) roles
$function$;

CREATE OR REPLACE FUNCTION public.project_grant_defaults()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.created_by AND org_role <> 'guest') THEN
        INSERT INTO project_grant (project_id, subject_type, user_id, role)
        VALUES (NEW.id, 'user', NEW.created_by, 'admin');
    END IF;
    INSERT INTO project_grant (project_id, subject_type, role) VALUES (NEW.id, 'everyone', 'editor');
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.project_grant_guest_check()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.role = 'admin' AND NEW.subject_type = 'user' AND EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id AND org_role = 'guest') THEN
        RAISE EXCEPTION 'A guest cannot be an administrator of a project. Make them an editor, or a member of the organization first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'project_grant_guest_not_admin';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.theme_unshared_default()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE org SET default_theme_id = NULL WHERE default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.org_default_theme_guard()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF NEW.default_theme_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM theme WHERE id = NEW.default_theme_id AND shared
    ) THEN
        RAISE EXCEPTION 'the organization''s default theme has to be one of its shared themes'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.link_type_project_reach()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM link_type t WHERE t.id = NEW.link_type_id
                   AND (t.project_id IS NULL OR t.project_id = NEW.project_id)) THEN
        RAISE EXCEPTION 'a project sets the organization''s link types and its own'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_project_reach';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.requirement_number()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    UPDATE project SET next_number = next_number + 1
    WHERE id = NEW.project_id
    RETURNING next_number - 1 INTO NEW.number;
    IF NEW.number IS NULL THEN
        RAISE EXCEPTION 'a requirement needs a project of its own organization'
            USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'requirement_project_fkey';
    END IF;
    IF NEW.rank IS NULL THEN
        NEW.rank := lpad(to_hex(NEW.number), 10, '0') || 'V';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.armature_connection_forgets_links()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM armature_remote_link;
    END IF;
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd
DROP FUNCTION link_type_seed();

ALTER POLICY app_user_installation ON app_user
    USING (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id))
    WITH CHECK (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id));
DROP POLICY app_user_admins_add ON app_user;
GRANT DELETE ON app_user TO exciter_app;
ALTER POLICY audit_log_admins_read ON audit_log
    USING ((SELECT EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = current_actor_id() AND m.org_role IN ('owner', 'admin'))));

DROP TRIGGER app_user_left ON app_user;
DROP FUNCTION app_user_left();
DROP TRIGGER app_user_role_guard ON app_user;
DROP FUNCTION app_user_role_guard();
DROP TRIGGER app_user_keeps_an_administrator ON app_user;
CREATE TRIGGER app_user_keeps_an_administrator BEFORE UPDATE OF is_active ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_keeps_an_administrator();
ALTER TABLE app_user DROP COLUMN role, DROP COLUMN role_source, DROP COLUMN joined_at;
