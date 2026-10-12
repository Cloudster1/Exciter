-- +goose Up
-- Projects, the homes of requirements (#11). A project's key prefixes every requirement key in it, so a key
-- that changes stays behind in project_key_alias and keeps resolving; a key is taken while any project has it
-- now or had it before, so an old key never points at two projects.
CREATE TABLE project (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id         uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    key            text NOT NULL,
    name           text NOT NULL,
    description    text NOT NULL DEFAULT '',
    -- The number the next requirement takes (#13); never handed out twice.
    next_number    integer NOT NULL DEFAULT 1,
    settings       jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- The key of the template the project began from (#24), empty for a blank one.
    template       text NOT NULL DEFAULT '',
    created_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    archived_at    timestamptz,
    archived_by    uuid REFERENCES app_user(id) ON DELETE SET NULL,
    CONSTRAINT project_key_shape CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$'),
    CONSTRAINT project_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT project_settings_object CHECK (jsonb_typeof(settings) = 'object'),
    CONSTRAINT project_next_number_positive CHECK (next_number > 0),
    CONSTRAINT project_org_id_key UNIQUE (org_id, id)
);

CREATE UNIQUE INDEX project_org_key_idx ON project (org_id, key);
CREATE INDEX project_org_name_idx ON project (org_id, lower(name));

CREATE TRIGGER project_set_updated_at BEFORE UPDATE ON project
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE project_key_alias (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    key        text NOT NULL,
    project_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, key),
    CONSTRAINT project_key_alias_shape CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$'),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE
);

CREATE INDEX project_key_alias_project_idx ON project_key_alias (project_id);

-- A key is one project's, current or former. The two tables cannot share a unique index, so whoever claims a
-- key takes a lock on it first and then looks in the other table: a project may take back a key it had, and an
-- alias may never be any project's current key. SECURITY DEFINER, since the claimant may not see the holder.
-- +goose StatementBegin
CREATE FUNCTION project_key_claim(org uuid, claimed text, claimant uuid, as_alias boolean) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('project_key/' || org::text || '/' || claimed, 0));
    IF (as_alias AND EXISTS (SELECT 1 FROM project WHERE org_id = org AND key = claimed))
       OR (NOT as_alias AND EXISTS (SELECT 1 FROM project_key_alias WHERE org_id = org AND key = claimed AND project_id <> claimant)) THEN
        RAISE EXCEPTION 'the key % is taken by another project', claimed
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'project_key_taken';
    END IF;
END;
$$;
-- +goose StatementEnd

-- Whether a key is taken in the caller's organization, for the form that suggests one; it says no more than
-- creating the project would.
-- +goose StatementBegin
CREATE FUNCTION project_key_taken(claimed text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM project WHERE org_id = current_org_id() AND key = claimed)
        OR EXISTS (SELECT 1 FROM project_key_alias WHERE org_id = current_org_id() AND key = claimed)
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION project_key_check() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM project_key_claim(NEW.org_id, NEW.key, NEW.id, false);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_key_check BEFORE INSERT OR UPDATE OF key ON project
    FOR EACH ROW EXECUTE FUNCTION project_key_check();

-- +goose StatementBegin
CREATE FUNCTION project_key_alias_check() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM project_key_claim(NEW.org_id, NEW.key, NEW.project_id, true);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_key_alias_check BEFORE INSERT OR UPDATE ON project_key_alias
    FOR EACH ROW EXECUTE FUNCTION project_key_alias_check();

-- The key a project leaves stays behind as an alias, and a former key taken back stops being one, whoever
-- changes it, so no path through SQL loses an address people may have written down.
-- +goose StatementBegin
CREATE FUNCTION project_key_keep() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.key IS DISTINCT FROM OLD.key THEN
        DELETE FROM project_key_alias WHERE org_id = NEW.org_id AND key = NEW.key AND project_id = NEW.id;
        INSERT INTO project_key_alias (org_id, key, project_id) VALUES (OLD.org_id, OLD.key, OLD.id);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_key_keep AFTER UPDATE OF key ON project
    FOR EACH ROW EXECUTE FUNCTION project_key_keep();

-- What a person may do in a project: view, comment, edit, approve or administer. Until #12 brings grants,
-- members view, comment and edit every project, owners and administrators hold everything, and guests hold
-- nothing; #12 replaces the body and every policy below follows. An archived project allows only view, and
-- administer, which is what unarchives and deletes it.
-- +goose StatementBegin
CREATE FUNCTION perm_project_holds(project uuid, actor uuid, permission text) RETURNS boolean
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

ALTER TABLE project           ENABLE ROW LEVEL SECURITY;
ALTER TABLE project           FORCE  ROW LEVEL SECURITY;
ALTER TABLE project_key_alias ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_key_alias FORCE  ROW LEVEL SECURITY;

CREATE POLICY project_tenant_isolation ON project
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY project_key_alias_tenant_isolation ON project_key_alias
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY project_admin_bypass ON project TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY project_key_alias_admin_bypass ON project_key_alias TO exciter_admin USING (true) WITH CHECK (true);

-- Through the app role a person sees the projects they may view, makes one only as an owner or administrator
-- of the organization and in their own name, changes one they administer, and deletes one only once archived.
CREATE POLICY project_visible ON project AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(id, current_actor_id(), 'view'));
CREATE POLICY project_made_by_admins ON project AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (current_actor_administers() AND created_by = current_actor_id());
CREATE POLICY project_changed_by_administrators ON project AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(id, current_actor_id(), 'administer'))
    WITH CHECK (perm_project_holds(id, current_actor_id(), 'administer'));
CREATE POLICY project_deleted_once_archived ON project AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (archived_at IS NOT NULL AND perm_project_holds(id, current_actor_id(), 'administer'));

-- An alias is seen with its project and written only by the triggers above, which run as their owner.
CREATE POLICY project_key_alias_visible ON project_key_alias AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));

GRANT SELECT, INSERT, UPDATE, DELETE ON project TO exciter_app, exciter_admin;
GRANT SELECT ON project_key_alias TO exciter_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON project_key_alias TO exciter_admin;
REVOKE INSERT, UPDATE, DELETE ON project_key_alias FROM exciter_app;

-- +goose Down
DROP TABLE IF EXISTS project_key_alias;
DROP TABLE IF EXISTS project;
DROP FUNCTION IF EXISTS perm_project_holds(uuid, uuid, text);
DROP FUNCTION IF EXISTS project_key_keep();
DROP FUNCTION IF EXISTS project_key_alias_check();
DROP FUNCTION IF EXISTS project_key_check();
DROP FUNCTION IF EXISTS project_key_taken(text);
DROP FUNCTION IF EXISTS project_key_claim(uuid, text, uuid, boolean);
