-- +goose Up
-- Field rules (#206), as docs/api-contract-m8.md agrees: a field is required from a status on, a select attribute's
-- choices narrow by another's, and a field is changed only by the roles its permits name in the statuses they name.
-- The service says which field and why; the database holds the choices and the permits for every writer.

-- An attribute's rules live with its definition: the status it is required from, the select attribute it depends on
-- with the options each of that one's options allows, and its permits, [{roles, statuses}].
ALTER TABLE attribute_def
    ADD COLUMN required_from uuid REFERENCES req_status(id) ON DELETE SET NULL,
    ADD COLUMN depends_on    uuid REFERENCES attribute_def(id) ON DELETE SET NULL,
    ADD COLUMN choices       jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN editable_by   jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT attribute_def_choices_map CHECK (jsonb_typeof(choices) = 'object'),
    ADD CONSTRAINT attribute_def_editable_by_list CHECK (jsonb_typeof(editable_by) = 'array'),
    ADD CONSTRAINT attribute_def_not_its_own CHECK (depends_on IS DISTINCT FROM id);

-- A fixed field's rules, one row per field of a project that has any.
CREATE TABLE field_rule (
    project_id    uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    field         text NOT NULL CHECK (field IN ('title', 'statement', 'rationale', 'type', 'owner', 'priority')),
    required_from uuid REFERENCES req_status(id) ON DELETE SET NULL,
    editable_by   jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(editable_by) = 'array'),
    updated_by    uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, field),
    -- A title and a statement are required always.
    CONSTRAINT field_rule_always_required CHECK (field NOT IN ('title', 'statement') OR required_from IS NULL)
);

ALTER TABLE field_rule ENABLE ROW LEVEL SECURITY;
ALTER TABLE field_rule FORCE  ROW LEVEL SECURITY;
CREATE POLICY field_rule_installation ON field_rule USING (true) WITH CHECK (true);
CREATE POLICY field_rule_readers ON field_rule AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY field_rule_setters ON field_rule AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY field_rule_changers ON field_rule AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id())) WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY field_rule_removers ON field_rule AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id()));
GRANT SELECT, INSERT, UPDATE, DELETE ON field_rule TO exciter_app, exciter_admin;

-- +goose StatementBegin
-- Whether a field's permits let the actor change it while the requirement is in a status: no permits leave it to
-- whoever may edit the requirement, and a permit with no statuses holds in every status.
CREATE FUNCTION field_permitted(permits jsonb, project uuid, status uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT jsonb_array_length(permits) = 0 OR EXISTS (
        SELECT 1 FROM jsonb_array_elements(permits) p
        WHERE (jsonb_typeof(p -> 'statuses') IS DISTINCT FROM 'array' OR jsonb_array_length(p -> 'statuses') = 0
               OR (p -> 'statuses') ? status::text)
          AND jsonb_typeof(p -> 'roles') = 'array'
          AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(p -> 'roles') r WHERE r = ANY (perm_project_roles(project, actor))))
$$;

-- A change of a field its permits keep from the person it is made for is refused, whoever writes it; a write that
-- acts for nobody, as the worker's, has no person to hold to.
CREATE FUNCTION requirement_fields_permitted() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    refused text;
BEGIN
    IF actor IS NULL OR NEW.kind = 'section' THEN
        RETURN NEW;
    END IF;
    SELECT f.field INTO refused
    FROM field_rule f
    WHERE f.project_id = OLD.project_id
      AND CASE f.field
            WHEN 'title' THEN NEW.title IS DISTINCT FROM OLD.title
            WHEN 'statement' THEN NEW.statement IS DISTINCT FROM OLD.statement
            WHEN 'rationale' THEN NEW.rationale IS DISTINCT FROM OLD.rationale
            WHEN 'type' THEN NEW.type_id IS DISTINCT FROM OLD.type_id
            WHEN 'owner' THEN NEW.owner_id IS DISTINCT FROM OLD.owner_id
            WHEN 'priority' THEN NEW.priority IS DISTINCT FROM OLD.priority
          END
      AND NOT field_permitted(f.editable_by, OLD.project_id, OLD.status_id, actor)
    LIMIT 1;
    IF refused IS NULL THEN
        SELECT a.name INTO refused
        FROM attribute_def a
        WHERE a.project_id = OLD.project_id
          AND (NEW.attributes -> a.id::text) IS DISTINCT FROM (OLD.attributes -> a.id::text)
          AND NOT field_permitted(a.editable_by, OLD.project_id, OLD.status_id, actor)
        LIMIT 1;
    END IF;
    IF refused IS NOT NULL THEN
        RAISE EXCEPTION 'You may not change % of this requirement in its status.', refused
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'field_not_yours';
    END IF;
    RETURN NEW;
END;
$$;

-- A dependent attribute holds only the options the option of the attribute it depends on allows; it is checked when
-- either of them changes, so a rule set over values already given refuses nothing until one of them moves.
CREATE FUNCTION requirement_choices_hold() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    refused text;
BEGIN
    SELECT a.name INTO refused
    FROM attribute_def a
    WHERE a.project_id = NEW.project_id AND a.depends_on IS NOT NULL
      AND NEW.attributes ? a.id::text
      AND (TG_OP = 'INSERT'
           OR (NEW.attributes -> a.id::text) IS DISTINCT FROM (OLD.attributes -> a.id::text)
           OR (NEW.attributes -> a.depends_on::text) IS DISTINCT FROM (OLD.attributes -> a.depends_on::text))
      AND a.choices ? (NEW.attributes ->> a.depends_on::text)
      AND EXISTS (
          SELECT 1 FROM jsonb_array_elements_text(
              CASE jsonb_typeof(NEW.attributes -> a.id::text) WHEN 'array' THEN NEW.attributes -> a.id::text
                   ELSE jsonb_build_array(NEW.attributes -> a.id::text) END) v
          WHERE NOT (a.choices -> (NEW.attributes ->> a.depends_on::text)) ? v)
    LIMIT 1;
    IF refused IS NOT NULL THEN
        RAISE EXCEPTION '% holds an option the attribute it depends on does not allow.', refused
            USING ERRCODE = 'check_violation', CONSTRAINT = 'attribute_value_choice';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_fields_permitted
    BEFORE UPDATE OF title, statement, rationale, type_id, owner_id, priority, attributes ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_fields_permitted();
CREATE TRIGGER requirement_choices_hold BEFORE INSERT OR UPDATE OF attributes ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_choices_hold();

-- +goose Down
DROP TRIGGER IF EXISTS requirement_choices_hold ON requirement;
DROP TRIGGER IF EXISTS requirement_fields_permitted ON requirement;
DROP FUNCTION IF EXISTS requirement_choices_hold();
DROP FUNCTION IF EXISTS requirement_fields_permitted();
DROP FUNCTION IF EXISTS field_permitted(jsonb, uuid, uuid, uuid);
DROP TABLE IF EXISTS field_rule;
ALTER TABLE attribute_def DROP CONSTRAINT IF EXISTS attribute_def_not_its_own,
    DROP CONSTRAINT IF EXISTS attribute_def_editable_by_list, DROP CONSTRAINT IF EXISTS attribute_def_choices_map,
    DROP COLUMN IF EXISTS editable_by, DROP COLUMN IF EXISTS choices, DROP COLUMN IF EXISTS depends_on,
    DROP COLUMN IF EXISTS required_from;
