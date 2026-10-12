-- +goose Up
-- Requirements (#13): a requirement or a section of a project, numbered from the project's counter, with its
-- normative content copied from the latest of its revisions, which are written once and never changed. What is
-- not normative is logged in requirement_change. A deleted requirement goes to the trash, named by trash_id.
CREATE TABLE requirement (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id         uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id     uuid NOT NULL,
    -- Taken from project.next_number by requirement_number, whatever an insert says.
    number         integer NOT NULL DEFAULT 0,
    kind           text NOT NULL DEFAULT 'requirement' CHECK (kind IN ('requirement', 'section')),
    title          text NOT NULL,
    statement      jsonb NOT NULL,
    -- Null is no rationale, which a section never has.
    rationale      jsonb,
    -- Values by attribute id (#14).
    attributes     jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- The number of the latest revision, moved by the revision that takes the next one.
    revision       integer NOT NULL DEFAULT 0,
    normative_hash text NOT NULL,
    owner_id       uuid,
    priority       text CHECK (priority IN ('must', 'should', 'could', 'wont')),
    created_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    trashed_at     timestamptz,
    trashed_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    trash_id       uuid,
    CONSTRAINT requirement_org_id_key UNIQUE (org_id, id),
    CONSTRAINT requirement_project_number_key UNIQUE (project_id, number),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    -- An owner who leaves the organization leaves the requirement without one.
    FOREIGN KEY (org_id, owner_id) REFERENCES org_member (org_id, user_id) ON DELETE SET NULL (owner_id),
    CONSTRAINT requirement_trash_fkey FOREIGN KEY (org_id, trash_id) REFERENCES requirement (org_id, id) ON DELETE CASCADE,
    CONSTRAINT requirement_title_shape CHECK (btrim(title) <> '' AND char_length(title) <= 300),
    CONSTRAINT requirement_statement_doc CHECK (jsonb_typeof(statement) = 'object'),
    CONSTRAINT requirement_attributes_object CHECK (jsonb_typeof(attributes) = 'object'),
    CONSTRAINT requirement_revision_not_negative CHECK (revision >= 0),
    CONSTRAINT requirement_section_plain CHECK (
        kind = 'requirement' OR (rationale IS NULL AND owner_id IS NULL AND priority IS NULL AND attributes = '{}'::jsonb)),
    CONSTRAINT requirement_trash_whole CHECK ((trashed_at IS NULL) = (trash_id IS NULL))
);

CREATE INDEX requirement_project_idx ON requirement (org_id, project_id, number);
CREATE INDEX requirement_owner_idx ON requirement (org_id, owner_id) WHERE owner_id IS NOT NULL;
CREATE INDEX requirement_trash_roots_idx ON requirement (org_id, project_id, trashed_at DESC) WHERE trash_id = id;
CREATE INDEX requirement_trash_items_idx ON requirement (org_id, trash_id) WHERE trash_id IS NOT NULL;

CREATE TRIGGER requirement_set_updated_at BEFORE UPDATE ON requirement
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Taking a number is no change to the project, so it leaves the project's updated_at alone.
DROP TRIGGER project_set_updated_at ON project;
CREATE TRIGGER project_set_updated_at BEFORE UPDATE ON project
    FOR EACH ROW WHEN (OLD.next_number = NEW.next_number) EXECUTE FUNCTION set_updated_at();

-- Numbers run 1, 2, 3 in a project with no gaps and are never handed out twice, whoever inserts: the project
-- row is locked, so two creates queue rather than both taking the same number.
-- +goose StatementBegin
CREATE FUNCTION requirement_number() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    UPDATE project SET next_number = next_number + 1
    WHERE org_id = NEW.org_id AND id = NEW.project_id
    RETURNING next_number - 1 INTO NEW.number;
    IF NEW.number IS NULL THEN
        RAISE EXCEPTION 'a requirement needs a project of its own organization'
            USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'requirement_project_fkey';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_number BEFORE INSERT ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_number();

-- The number never changes, and neither does the project but by a move (#15), which brings its own function.
-- +goose StatementBegin
CREATE FUNCTION requirement_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.number <> OLD.number OR NEW.project_id <> OLD.project_id OR NEW.kind <> OLD.kind THEN
        RAISE EXCEPTION 'a requirement keeps its number, its project and its kind'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_fixed BEFORE UPDATE OF number, project_id, kind ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_fixed();

-- A revision is written once and never changed; the requirement row carries a copy of the latest one, so
-- nothing that reads a requirement joins the history.
CREATE TABLE requirement_revision (
    org_id         uuid NOT NULL,
    requirement_id uuid NOT NULL,
    number         integer NOT NULL,
    title          text NOT NULL,
    statement      jsonb NOT NULL,
    rationale      jsonb,
    attributes     jsonb NOT NULL DEFAULT '{}'::jsonb,
    hash           text NOT NULL,
    comment        text NOT NULL DEFAULT '',
    restored_from  integer,
    created_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, requirement_id, number),
    FOREIGN KEY (org_id, requirement_id) REFERENCES requirement (org_id, id) ON DELETE CASCADE,
    CONSTRAINT requirement_revision_number_positive CHECK (number > 0),
    CONSTRAINT requirement_revision_comment_length CHECK (char_length(comment) <= 500),
    CONSTRAINT requirement_revision_restores_older CHECK (restored_from > 0 AND restored_from < number)
);

-- Revisions run 1, 2, 3 with no gaps, whoever writes them: each is the one after the requirement's current
-- revision, which it then becomes. The requirement row is locked so two saves queue.
-- +goose StatementBegin
CREATE FUNCTION requirement_revision_next() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    current integer;
BEGIN
    SELECT revision INTO current FROM requirement WHERE org_id = NEW.org_id AND id = NEW.requirement_id FOR UPDATE;
    IF current IS NULL OR NEW.number <> current + 1 THEN
        RAISE EXCEPTION 'revision % of a requirement is not the one after its current revision %', NEW.number, current
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_revision_in_sequence';
    END IF;
    UPDATE requirement SET revision = NEW.number WHERE org_id = NEW.org_id AND id = NEW.requirement_id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_revision_in_sequence BEFORE INSERT ON requirement_revision
    FOR EACH ROW EXECUTE FUNCTION requirement_revision_next();

-- What changed without a revision: the field, what it was and became as a reader sees them, and why when it
-- was not the actor's own choice. Append only, as the history is.
CREATE TABLE requirement_change (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id         uuid NOT NULL,
    requirement_id uuid NOT NULL,
    field          text NOT NULL CHECK (field IN ('status', 'owner', 'priority', 'labels', 'place', 'key', 'link', 'attachment', 'trash')),
    from_value     text NOT NULL DEFAULT '',
    to_value       text NOT NULL DEFAULT '',
    reason         text NOT NULL DEFAULT '',
    actor_id       uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, requirement_id) REFERENCES requirement (org_id, id) ON DELETE CASCADE
);

CREATE INDEX requirement_change_requirement_idx ON requirement_change (org_id, requirement_id, created_at DESC);

-- A key a requirement left on a move to another project (#15), which keeps resolving.
CREATE TABLE requirement_old_key (
    org_id         uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    key            text NOT NULL,
    requirement_id uuid NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, key),
    FOREIGN KEY (org_id, requirement_id) REFERENCES requirement (org_id, id) ON DELETE CASCADE,
    CONSTRAINT requirement_old_key_shape CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}-R[1-9][0-9]*$')
);

-- Whether the actor may read a requirement: view its project, and, while it is in the trash, edit there,
-- since only editors open the trash.
-- +goose StatementBegin
CREATE FUNCTION perm_requirement_viewable(requirement uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM requirement r
        WHERE r.id = perm_requirement_viewable.requirement AND r.org_id = current_org_id()
          AND perm_project_holds(r.project_id, actor, 'view')
          AND (r.trashed_at IS NULL OR perm_project_holds(r.project_id, actor, 'edit')))
$$;
-- +goose StatementEnd

-- Whether the actor may change a requirement: edit in its project, which an archived project allows nobody.
-- +goose StatementBegin
CREATE FUNCTION perm_requirement_editable(requirement uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM requirement r
        WHERE r.id = perm_requirement_editable.requirement AND r.org_id = current_org_id()
          AND perm_project_holds(r.project_id, actor, 'edit'))
$$;
-- +goose StatementEnd

ALTER TABLE requirement          ENABLE ROW LEVEL SECURITY;
ALTER TABLE requirement          FORCE  ROW LEVEL SECURITY;
ALTER TABLE requirement_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE requirement_revision FORCE  ROW LEVEL SECURITY;
ALTER TABLE requirement_change   ENABLE ROW LEVEL SECURITY;
ALTER TABLE requirement_change   FORCE  ROW LEVEL SECURITY;
ALTER TABLE requirement_old_key  ENABLE ROW LEVEL SECURITY;
ALTER TABLE requirement_old_key  FORCE  ROW LEVEL SECURITY;

CREATE POLICY requirement_tenant_isolation ON requirement
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY requirement_revision_tenant_isolation ON requirement_revision
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY requirement_change_tenant_isolation ON requirement_change
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY requirement_old_key_tenant_isolation ON requirement_old_key
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY requirement_admin_bypass ON requirement TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY requirement_revision_admin_bypass ON requirement_revision TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY requirement_change_admin_bypass ON requirement_change TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY requirement_old_key_admin_bypass ON requirement_old_key TO exciter_admin USING (true) WITH CHECK (true);

-- Through the app role a person reads what they may view, writes in projects they edit in their own name,
-- and purges from the trash only as the project's administrator while it is not archived.
CREATE POLICY requirement_visible ON requirement AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view')
           AND (trashed_at IS NULL OR perm_project_holds(project_id, current_actor_id(), 'edit')));
CREATE POLICY requirement_written_by_editors ON requirement AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'edit') AND created_by = current_actor_id());
CREATE POLICY requirement_changed_by_editors ON requirement AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'edit'))
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'edit'));
CREATE POLICY requirement_purged_by_administrators ON requirement AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (trashed_at IS NOT NULL AND perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id));

CREATE POLICY requirement_revision_visible ON requirement_revision AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY requirement_revision_written_by_editors ON requirement_revision AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_requirement_editable(requirement_id, current_actor_id()) AND created_by = current_actor_id());
CREATE POLICY requirement_change_visible ON requirement_change AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY requirement_change_written_by_editors ON requirement_change AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_requirement_editable(requirement_id, current_actor_id()) AND actor_id = current_actor_id());
CREATE POLICY requirement_old_key_visible ON requirement_old_key AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON requirement TO exciter_app, exciter_admin;
-- History is append only for the app: no UPDATE, no DELETE, and rows leave only with their requirement. The
-- default privileges grant everything, so it is taken back first.
REVOKE ALL ON requirement_revision, requirement_change, requirement_old_key FROM exciter_app;
GRANT SELECT, INSERT ON requirement_revision, requirement_change TO exciter_app;
GRANT SELECT ON requirement_old_key TO exciter_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON requirement_revision, requirement_change, requirement_old_key TO exciter_admin;

-- +goose Down
DROP TABLE IF EXISTS requirement_old_key;
DROP TABLE IF EXISTS requirement_change;
DROP TABLE IF EXISTS requirement_revision;
DROP TABLE IF EXISTS requirement;
DROP FUNCTION IF EXISTS perm_requirement_editable(uuid, uuid);
DROP FUNCTION IF EXISTS perm_requirement_viewable(uuid, uuid);
DROP FUNCTION IF EXISTS requirement_revision_next();
DROP FUNCTION IF EXISTS requirement_fixed();
DROP FUNCTION IF EXISTS requirement_number();
DROP TRIGGER IF EXISTS project_set_updated_at ON project;
CREATE TRIGGER project_set_updated_at BEFORE UPDATE ON project
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
