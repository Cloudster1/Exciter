-- +goose Up
-- ReqIF import and export (#44). An exchange goes out and comes back, so what each ReqIF identifier became in a
-- project is kept, and a file that returns changes what it made instead of making it again.

-- What an identifier of a ReqIF file names in a project: a requirement or section (object), a requirement type
-- (type), an attribute on one type (attribute, scoped by the type), an option of an attribute (option), a link
-- type (relation_type), or the section or the project itself a specification is (specification).
CREATE TABLE reqif_identity (
    project_id uuid NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('object', 'type', 'attribute', 'option', 'relation_type', 'specification')),
    identifier text NOT NULL,
    target_id  uuid NOT NULL,
    -- The type an attribute's identifier belongs to: ReqIF defines an attribute on each type, a project once.
    scope_id   uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, kind, identifier),
    CONSTRAINT reqif_identity_identifier CHECK (btrim(identifier) <> '' AND length(identifier) <= 255),
    CONSTRAINT reqif_identity_scope CHECK ((scope_id IS NOT NULL) = (kind = 'attribute')),
    CONSTRAINT reqif_identity_target UNIQUE NULLS NOT DISTINCT (project_id, kind, target_id, scope_id)
);

-- An identity names something of its own project, so an identifier never reaches into another.
-- +goose StatementBegin
CREATE FUNCTION reqif_identity_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    found boolean;
BEGIN
    found := CASE NEW.kind
        WHEN 'object' THEN EXISTS (SELECT 1 FROM requirement WHERE id = NEW.target_id AND project_id = NEW.project_id)
        WHEN 'specification' THEN NEW.target_id = NEW.project_id
            OR EXISTS (SELECT 1 FROM requirement WHERE id = NEW.target_id AND project_id = NEW.project_id AND kind = 'section')
        WHEN 'type' THEN EXISTS (SELECT 1 FROM requirement_type WHERE id = NEW.target_id AND project_id = NEW.project_id)
        WHEN 'attribute' THEN EXISTS (SELECT 1 FROM attribute_def WHERE id = NEW.target_id AND project_id = NEW.project_id)
            AND EXISTS (SELECT 1 FROM requirement_type WHERE id = NEW.scope_id AND project_id = NEW.project_id)
        WHEN 'option' THEN EXISTS (SELECT 1 FROM attribute_def d, jsonb_array_elements(d.options) o
                                   WHERE d.project_id = NEW.project_id AND o ->> 'id' = NEW.target_id::text)
        WHEN 'relation_type' THEN EXISTS (SELECT 1 FROM link_type WHERE id = NEW.target_id
                                          AND (project_id IS NULL OR project_id = NEW.project_id))
    END;
    IF NOT found THEN
        RAISE EXCEPTION 'reqif_identity_elsewhere' USING ERRCODE = 'check_violation',
            DETAIL = 'A ReqIF identifier names something of its own project.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER reqif_identity_guard BEFORE INSERT OR UPDATE ON reqif_identity
    FOR EACH ROW EXECUTE FUNCTION reqif_identity_guard();

-- A ReqIF file on its way into a project: what it holds, read once at upload, how its types, attributes and
-- link types are mapped, and how far the worker has come. The worker runs it as its maker, so whatever it writes
-- is held to that person's rights like any other write.
CREATE TABLE reqif_import (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id  uuid NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    created_by  uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    file_name   text NOT NULL,
    format      text NOT NULL CHECK (format IN ('reqif', 'reqifz')),
    -- The file as reqifio.File, its images' bytes aside in reqif_import_object.
    content     jsonb NOT NULL,
    total       integer NOT NULL,
    state       text NOT NULL DEFAULT 'uploaded'
        CHECK (state IN ('uploaded', 'queued', 'running', 'done', 'failed', 'cancelled')),
    -- An identifier of the file's types, attributes and link types, to the id of what it becomes, create or skip.
    choices     jsonb NOT NULL DEFAULT '{}'::jsonb,
    preview     jsonb NOT NULL DEFAULT '{}'::jsonb,
    done        integer NOT NULL DEFAULT 0,
    imported    text[] NOT NULL DEFAULT '{}',
    updated     text[] NOT NULL DEFAULT '{}',
    unchanged   integer NOT NULL DEFAULT 0,
    refused     jsonb NOT NULL DEFAULT '[]'::jsonb,
    error       text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz,
    CONSTRAINT reqif_import_file_name CHECK (btrim(file_name) <> ''),
    CONSTRAINT reqif_import_content CHECK (jsonb_typeof(content) = 'object'),
    CONSTRAINT reqif_import_total CHECK (total BETWEEN 0 AND 5000),
    CONSTRAINT reqif_import_progress CHECK (done BETWEEN 0 AND total AND unchanged >= 0)
);

CREATE INDEX reqif_import_project_idx ON reqif_import (project_id, created_at DESC);

-- The file never changes once uploaded, the choices only until the import is queued, and an import moves only
-- forward: uploaded, queued, running, then done, failed or cancelled.
-- +goose StatementBegin
CREATE FUNCTION reqif_import_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.project_id, NEW.created_by, NEW.file_name, NEW.format, NEW.content, NEW.total, NEW.created_at)
       IS DISTINCT FROM (OLD.project_id, OLD.created_by, OLD.file_name, OLD.format, OLD.content, OLD.total, OLD.created_at) THEN
        RAISE EXCEPTION 'reqif_import_file_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The file of an import never changes; upload it again instead.';
    END IF;
    IF OLD.state <> 'uploaded' AND NEW.choices IS DISTINCT FROM OLD.choices THEN
        RAISE EXCEPTION 'reqif_import_choices_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The choices of an import that started never change.';
    END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
           (OLD.state = 'uploaded' AND NEW.state IN ('queued', 'cancelled'))
        OR (OLD.state = 'queued' AND NEW.state IN ('running', 'cancelled'))
        OR (OLD.state = 'running' AND NEW.state IN ('done', 'failed', 'cancelled'))) THEN
        RAISE EXCEPTION 'reqif_import_forward' USING ERRCODE = 'check_violation',
            DETAIL = 'An import moves from uploaded to queued to running, and then ends.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER reqif_import_guard BEFORE UPDATE ON reqif_import
    FOR EACH ROW EXECUTE FUNCTION reqif_import_guard();

-- The files of an archive its rich text shows, kept with the import until the worker makes them attachments, so
-- the API and the worker need share nothing but the database. They go with their import.
CREATE TABLE reqif_import_object (
    import_id    uuid NOT NULL REFERENCES reqif_import (id) ON DELETE CASCADE,
    path         text NOT NULL,
    content_type text NOT NULL,
    data         bytea NOT NULL,
    PRIMARY KEY (import_id, path),
    -- 50 MB, an attachment's default limit, which reqifio.MaxImageBytes matches.
    CONSTRAINT reqif_import_object_size CHECK (octet_length(data) BETWEEN 1 AND 52428800)
);

ALTER TABLE reqif_identity      ENABLE ROW LEVEL SECURITY;
ALTER TABLE reqif_identity      FORCE  ROW LEVEL SECURITY;
ALTER TABLE reqif_import        ENABLE ROW LEVEL SECURITY;
ALTER TABLE reqif_import        FORCE  ROW LEVEL SECURITY;
ALTER TABLE reqif_import_object ENABLE ROW LEVEL SECURITY;
ALTER TABLE reqif_import_object FORCE  ROW LEVEL SECURITY;

CREATE POLICY reqif_identity_installation ON reqif_identity USING (true) WITH CHECK (true);
CREATE POLICY reqif_import_installation ON reqif_import USING (true) WITH CHECK (true);
CREATE POLICY reqif_import_object_installation ON reqif_import_object USING (true) WITH CHECK (true);

-- Whoever views a project reads what its identifiers name, since an export writes them; only who administers it
-- ties an identifier to something, since only they import.
CREATE POLICY reqif_identity_readers ON reqif_identity AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY reqif_identity_tied ON reqif_identity AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY reqif_identity_retied ON reqif_identity AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer'))
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY reqif_identity_untied ON reqif_identity AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer'));

-- An import is made by who administers the project, in their own name; it is its maker's, and the project's
-- administrators', to read and to stop, and its maker's alone to map and start, which the worker does as them.
CREATE POLICY reqif_import_seen ON reqif_import AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY reqif_import_made_by_administrators ON reqif_import AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND state = 'uploaded' AND done = 0
                AND perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY reqif_import_changed ON reqif_import AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'))
    WITH CHECK (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY reqif_import_removed ON reqif_import AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'));

-- A file of an import goes with it: seen with it, added by its maker while it is uploaded, and removed with it.
CREATE POLICY reqif_import_object_seen ON reqif_import_object AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM reqif_import j WHERE j.id = import_id));
CREATE POLICY reqif_import_object_added ON reqif_import_object AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM reqif_import j WHERE j.id = import_id AND j.created_by = current_actor_id()
                        AND j.state = 'uploaded'));
CREATE POLICY reqif_import_object_removed ON reqif_import_object AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (EXISTS (SELECT 1 FROM reqif_import j WHERE j.id = import_id));

GRANT SELECT, INSERT, UPDATE, DELETE ON reqif_identity, reqif_import, reqif_import_object TO exciter_admin;
REVOKE ALL ON reqif_identity, reqif_import, reqif_import_object FROM exciter_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON reqif_identity, reqif_import TO exciter_app;
-- A file of an import never changes once kept.
GRANT SELECT, INSERT, DELETE ON reqif_import_object TO exciter_app;

-- +goose Down
DROP TABLE IF EXISTS reqif_import_object;
DROP TABLE IF EXISTS reqif_import;
DROP FUNCTION IF EXISTS reqif_import_guard();
DROP TABLE IF EXISTS reqif_identity;
DROP FUNCTION IF EXISTS reqif_identity_guard();
