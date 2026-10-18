-- +goose Up
-- Word templates (#214), as docs/api-contract-m11.md agrees: a project's administrators upload a Word file whose
-- styles, cover, headers and footers exports take, choose which fields a requirement shows and what it brings, and may
-- offer it to every project. Its bytes are in the object store under a key the database names, and go when it does.

CREATE TABLE export_template (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id   uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    -- exporttemplate.MaxNameLength and MaxDescriptionLength.
    name         text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 80),
    description  text NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    shared       boolean NOT NULL DEFAULT false,
    file_name    text NOT NULL CHECK (btrim(file_name) <> ''),
    size         bigint NOT NULL CHECK (size > 0),
    sha256       text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    object_key   text NOT NULL DEFAULT '',
    placeholders text[] NOT NULL DEFAULT '{}',
    fields       jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(fields) = 'array'),
    -- A new template brings links, as an export without one does.
    include      jsonb NOT NULL DEFAULT '{"links": true}'::jsonb CHECK (jsonb_typeof(include) = 'object'),
    created_by   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX export_template_name_idx ON export_template (project_id, lower(name));
CREATE INDEX export_template_shared_idx ON export_template (shared) WHERE shared;

-- +goose StatementBegin
-- A template's file is named after the template, under the installation's prefix, whatever an insert says.
CREATE FUNCTION export_template_key() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.object_key := 'org/' || (SELECT id FROM installation) || '/template/' || NEW.id;
    RETURN NEW;
END;
$$;

-- A template keeps its project and its file; a new file is a new template.
CREATE FUNCTION export_template_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.project_id, NEW.file_name, NEW.size, NEW.sha256, NEW.object_key, NEW.placeholders, NEW.created_by, NEW.created_at)
       IS DISTINCT FROM (OLD.project_id, OLD.file_name, OLD.size, OLD.sha256, OLD.object_key, OLD.placeholders, OLD.created_by, OLD.created_at) THEN
        RAISE EXCEPTION 'a template keeps its project and its file; upload a new one instead'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'export_template_fixed';
    END IF;
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

-- The key of a template's project, for whoever may read the template: a shared one names its project to readers
-- the project itself is hidden from.
CREATE FUNCTION export_template_project_key(of_template uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT p.key FROM export_template t JOIN project p ON p.id = t.project_id
    WHERE t.id = of_template AND (t.shared OR perm_project_holds(t.project_id, current_actor_id(), 'view'));
$$;

-- A template deleted leaves its file for the worker's reaper, as an attachment does.
CREATE FUNCTION export_template_tombstone() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    INSERT INTO attachment_tombstone (object_key) VALUES (OLD.object_key) ON CONFLICT DO NOTHING;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER export_template_key BEFORE INSERT ON export_template FOR EACH ROW EXECUTE FUNCTION export_template_key();
CREATE TRIGGER export_template_fixed BEFORE UPDATE ON export_template FOR EACH ROW EXECUTE FUNCTION export_template_fixed();
CREATE TRIGGER export_template_tombstone AFTER DELETE ON export_template FOR EACH ROW EXECUTE FUNCTION export_template_tombstone();

ALTER TABLE export_template ENABLE ROW LEVEL SECURITY;
ALTER TABLE export_template FORCE  ROW LEVEL SECURITY;
CREATE POLICY export_template_installation ON export_template USING (true) WITH CHECK (true);
-- Read by its project's readers, and by everybody signed in once shared.
CREATE POLICY export_template_readers ON export_template AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view') OR (shared AND current_actor_id() IS NOT NULL));
-- Written by its project's administrators.
CREATE POLICY export_template_makers ON export_template AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY export_template_changers ON export_template AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer'))
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY export_template_removers ON export_template AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer'));

GRANT SELECT, INSERT, UPDATE, DELETE ON export_template TO exciter_admin;
REVOKE ALL ON export_template FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON export_template TO exciter_app;
GRANT UPDATE (name, description, shared, fields, include) ON export_template TO exciter_app;
REVOKE ALL ON FUNCTION export_template_project_key(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION export_template_project_key(uuid) TO exciter_app, exciter_admin;

-- +goose Down
DROP FUNCTION export_template_project_key(uuid);
DROP TABLE export_template;
DROP FUNCTION export_template_tombstone();
DROP FUNCTION export_template_fixed();
DROP FUNCTION export_template_key();
