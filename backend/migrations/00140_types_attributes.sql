-- +goose Up
-- Requirement types and attributes (#14): the kinds of requirement a project tells apart, and what it records
-- about them. A value lives in requirement.attributes under its attribute's id, and a revision copies the
-- type and the values it had. What was ever used is retired rather than deleted, so history keeps it.
CREATE TABLE requirement_type (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id  uuid NOT NULL,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    icon        text NOT NULL,
    colour      text NOT NULL,
    position    integer NOT NULL,
    retired     boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT requirement_type_org_id_key UNIQUE (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    CONSTRAINT requirement_type_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 60),
    CONSTRAINT requirement_type_icon_shape CHECK (icon ~ '^[a-z][a-z0-9-]{0,39}$'),
    CONSTRAINT requirement_type_colour_shape CHECK (colour ~ '^#[0-9a-f]{6}$'),
    CONSTRAINT requirement_type_position_not_negative CHECK (position >= 0)
);

CREATE UNIQUE INDEX requirement_type_name_idx ON requirement_type (project_id, lower(name));

CREATE TRIGGER requirement_type_set_updated_at BEFORE UPDATE ON requirement_type
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Options are a list of {id, name, retired} kept in the attribute's order; values name an option by its id, so
-- renaming one renames it everywhere, history included.
CREATE TABLE attribute_def (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id  uuid NOT NULL,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    kind        text NOT NULL CHECK (kind IN ('text', 'richText', 'number', 'select', 'multiSelect', 'date', 'user', 'boolean', 'url')),
    required    boolean NOT NULL DEFAULT false,
    options     jsonb NOT NULL DEFAULT '[]'::jsonb,
    position    integer NOT NULL,
    retired     boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT attribute_def_org_id_key UNIQUE (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    CONSTRAINT attribute_def_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 60),
    CONSTRAINT attribute_def_options_list CHECK (jsonb_typeof(options) = 'array'),
    CONSTRAINT attribute_def_position_not_negative CHECK (position >= 0)
);

CREATE UNIQUE INDEX attribute_def_name_idx ON attribute_def (project_id, lower(name));

CREATE TRIGGER attribute_def_set_updated_at BEFORE UPDATE ON attribute_def
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The kind is what every stored value was checked against, so it never changes.
-- +goose StatementBegin
CREATE FUNCTION attribute_def_kind_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.kind <> OLD.kind OR NEW.project_id <> OLD.project_id THEN
        RAISE EXCEPTION 'an attribute keeps its kind and its project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'attribute_def_kind_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER attribute_def_kind_fixed BEFORE UPDATE OF kind, project_id ON attribute_def
    FOR EACH ROW EXECUTE FUNCTION attribute_def_kind_fixed();

-- The types an attribute shows on; none is every type.
CREATE TABLE attribute_def_type (
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    attribute_id uuid NOT NULL,
    type_id      uuid NOT NULL,
    PRIMARY KEY (attribute_id, type_id),
    FOREIGN KEY (org_id, attribute_id) REFERENCES attribute_def (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, type_id) REFERENCES requirement_type (org_id, id) ON DELETE CASCADE
);

CREATE INDEX attribute_def_type_type_idx ON attribute_def_type (org_id, type_id);

-- A requirement and each revision name their type; a type they name cannot be deleted, only retired.
ALTER TABLE requirement
    ADD COLUMN type_id uuid,
    ADD CONSTRAINT requirement_type_fkey FOREIGN KEY (org_id, type_id) REFERENCES requirement_type (org_id, id),
    ADD CONSTRAINT requirement_section_untyped CHECK (kind = 'requirement' OR type_id IS NULL);
ALTER TABLE requirement_revision
    ADD COLUMN type_id uuid,
    ADD CONSTRAINT requirement_revision_type_fkey FOREIGN KEY (org_id, type_id) REFERENCES requirement_type (org_id, id);

CREATE INDEX requirement_type_id_idx ON requirement (org_id, type_id) WHERE type_id IS NOT NULL;
CREATE INDEX requirement_revision_type_id_idx ON requirement_revision (org_id, type_id) WHERE type_id IS NOT NULL;
CREATE INDEX requirement_attributes_idx ON requirement USING gin (attributes);

-- Whose attribute and type a row names: its own project's, which a policy alone cannot see, since it would
-- have to read the attribute and the type through the reader's own rights.
-- +goose StatementBegin
CREATE FUNCTION requirement_attributes_check() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    project uuid;
    stray   text;
BEGIN
    IF TG_TABLE_NAME = 'requirement' THEN
        project := NEW.project_id;
    ELSE
        SELECT r.project_id INTO project FROM requirement r WHERE r.org_id = NEW.org_id AND r.id = NEW.requirement_id;
    END IF;
    IF NEW.type_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM requirement_type t WHERE t.org_id = NEW.org_id AND t.id = NEW.type_id AND t.project_id = project) THEN
        RAISE EXCEPTION 'a requirement takes a type of its own project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_type_same_project';
    END IF;
    SELECT k INTO stray FROM jsonb_object_keys(NEW.attributes) AS k
    WHERE NOT EXISTS (SELECT 1 FROM attribute_def a WHERE a.org_id = NEW.org_id AND a.project_id = project AND a.id::text = k)
    LIMIT 1;
    IF stray IS NOT NULL THEN
        RAISE EXCEPTION 'attribute % is not one of the requirement''s project', stray
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_attributes_same_project';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_attributes_check BEFORE INSERT OR UPDATE OF attributes, type_id ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_attributes_check();
CREATE TRIGGER requirement_revision_attributes_check BEFORE INSERT ON requirement_revision
    FOR EACH ROW EXECUTE FUNCTION requirement_attributes_check();

-- Every project has a type to start with, which is its default until another is chosen; a template (#24) may
-- put its own in its place.
-- +goose StatementBegin
CREATE FUNCTION project_type_default() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    made uuid;
BEGIN
    INSERT INTO requirement_type (org_id, project_id, name, icon, colour, position)
    VALUES (NEW.org_id, NEW.id, 'Requirement', 'requirement', '#4f6bed', 0) RETURNING id INTO made;
    UPDATE project SET settings = jsonb_set(settings, '{defaultTypeId}', to_jsonb(made::text)) WHERE id = NEW.id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_type_default AFTER INSERT ON project
    FOR EACH ROW EXECUTE FUNCTION project_type_default();

-- Projects made before types get the one type every project starts with. Their requirements keep no type, so
-- what each of their revisions says stays as it was.
WITH made AS (
    INSERT INTO requirement_type (org_id, project_id, name, icon, colour, position)
    SELECT org_id, id, 'Requirement', 'requirement', '#4f6bed', 0 FROM project
    RETURNING id, project_id)
UPDATE project p SET settings = jsonb_set(p.settings, '{defaultTypeId}', to_jsonb(made.id::text))
FROM made WHERE made.project_id = p.id;

-- Whether a project takes the change of its types and attributes: administered by the actor, and not archived.
-- +goose StatementBegin
CREATE FUNCTION perm_project_shaped(project uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_project_holds(project, actor, 'administer') AND project_open(project)
$$;
-- +goose StatementEnd

-- The project an attribute belongs to, for the policies of the table that names its types.
-- +goose StatementBegin
CREATE FUNCTION attribute_project(attribute uuid) RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT project_id FROM attribute_def WHERE id = attribute AND org_id = current_org_id()
$$;
-- +goose StatementEnd

-- How many requirements and revisions have a type, or a value of an attribute, trashed or not and whoever
-- asks: what decides whether it may go is everything that names it, not what the asker sees.
-- +goose StatementBegin
CREATE FUNCTION type_usage(type uuid) RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT (SELECT count(*) FROM requirement WHERE org_id = current_org_id() AND type_id = type_usage.type)::int
         + (SELECT count(*) FROM requirement_revision WHERE org_id = current_org_id() AND type_id = type_usage.type)::int
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION attribute_usage(attribute uuid) RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT (SELECT count(*) FROM requirement WHERE org_id = current_org_id() AND attributes ? attribute_usage.attribute::text)::int
         + (SELECT count(*) FROM requirement_revision WHERE org_id = current_org_id() AND attributes ? attribute_usage.attribute::text)::int
$$;
-- +goose StatementEnd

ALTER TABLE requirement_type   ENABLE ROW LEVEL SECURITY;
ALTER TABLE requirement_type   FORCE  ROW LEVEL SECURITY;
ALTER TABLE attribute_def      ENABLE ROW LEVEL SECURITY;
ALTER TABLE attribute_def      FORCE  ROW LEVEL SECURITY;
ALTER TABLE attribute_def_type ENABLE ROW LEVEL SECURITY;
ALTER TABLE attribute_def_type FORCE  ROW LEVEL SECURITY;

CREATE POLICY requirement_type_tenant_isolation ON requirement_type
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY attribute_def_tenant_isolation ON attribute_def
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY attribute_def_type_tenant_isolation ON attribute_def_type
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY requirement_type_admin_bypass ON requirement_type TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY attribute_def_admin_bypass ON attribute_def TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY attribute_def_type_admin_bypass ON attribute_def_type TO exciter_admin USING (true) WITH CHECK (true);

-- Through the app role whoever views a project reads its types and attributes, and its administrators change
-- them while it is not archived.
CREATE POLICY requirement_type_visible ON requirement_type AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY requirement_type_added ON requirement_type AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY requirement_type_changed ON requirement_type AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id())) WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY requirement_type_removed ON requirement_type AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY attribute_def_visible ON attribute_def AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY attribute_def_added ON attribute_def AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY attribute_def_changed ON attribute_def AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id())) WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY attribute_def_removed ON attribute_def AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY attribute_def_type_visible ON attribute_def_type AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(attribute_project(attribute_id), current_actor_id(), 'view'));
CREATE POLICY attribute_def_type_added ON attribute_def_type AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_shaped(attribute_project(attribute_id), current_actor_id()));
CREATE POLICY attribute_def_type_removed ON attribute_def_type AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_shaped(attribute_project(attribute_id), current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON requirement_type, attribute_def, attribute_def_type TO exciter_app, exciter_admin;

-- +goose Down
DROP TRIGGER IF EXISTS project_type_default ON project;
DROP FUNCTION IF EXISTS project_type_default();
DROP TRIGGER IF EXISTS requirement_revision_attributes_check ON requirement_revision;
DROP TRIGGER IF EXISTS requirement_attributes_check ON requirement;
DROP FUNCTION IF EXISTS requirement_attributes_check();
DROP INDEX IF EXISTS requirement_attributes_idx;
ALTER TABLE requirement_revision DROP COLUMN IF EXISTS type_id;
ALTER TABLE requirement DROP CONSTRAINT IF EXISTS requirement_section_untyped, DROP COLUMN IF EXISTS type_id;
DROP TABLE IF EXISTS attribute_def_type;
DROP TABLE IF EXISTS attribute_def;
DROP TABLE IF EXISTS requirement_type;
DROP FUNCTION IF EXISTS attribute_def_kind_fixed();
DROP FUNCTION IF EXISTS type_usage(uuid);
DROP FUNCTION IF EXISTS attribute_usage(uuid);
DROP FUNCTION IF EXISTS attribute_project(uuid);
DROP FUNCTION IF EXISTS perm_project_shaped(uuid, uuid);
UPDATE project SET settings = settings - 'defaultTypeId';
