-- +goose Up
-- Test cases (#250), as docs/api-contract-m12.md agrees: a requirement type may be marked as test cases, and a
-- requirement of such a type carries preconditions, ordered steps and parameters besides, in every revision.

-- requirement.MaxSteps and requirement.MaxParameters; the shape of each step and parameter is checked in Go.
ALTER TABLE requirement_type
    ADD COLUMN role text NOT NULL DEFAULT 'requirement' CHECK (role IN ('requirement', 'testCase'));

ALTER TABLE requirement
    ADD COLUMN preconditions jsonb CHECK (preconditions IS NULL OR jsonb_typeof(preconditions) = 'object'),
    ADD COLUMN steps         jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(steps) = 'array' AND jsonb_array_length(steps) <= 100),
    ADD COLUMN parameters    jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(parameters) = 'array' AND jsonb_array_length(parameters) <= 10);

ALTER TABLE requirement_revision
    ADD COLUMN preconditions jsonb CHECK (preconditions IS NULL OR jsonb_typeof(preconditions) = 'object'),
    ADD COLUMN steps         jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(steps) = 'array' AND jsonb_array_length(steps) <= 100),
    ADD COLUMN parameters    jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(parameters) = 'array' AND jsonb_array_length(parameters) <= 10);

-- +goose StatementBegin
-- Whether a requirement's row holds anything only a test case holds.
CREATE FUNCTION requirement_holds_test(preconditions jsonb, steps jsonb, parameters jsonb) RETURNS boolean
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT preconditions IS NOT NULL OR jsonb_array_length(steps) > 0 OR jsonb_array_length(parameters) > 0
$$;

-- Preconditions, steps and parameters belong to a requirement, never a section, of a type marked as test cases.
-- A revision copies its requirement's row, so holding the row holds every revision written from it.
CREATE FUNCTION requirement_test_case_fields() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF requirement_holds_test(NEW.preconditions, NEW.steps, NEW.parameters)
       AND (NEW.kind <> 'requirement' OR NOT EXISTS (
           SELECT 1 FROM requirement_type t WHERE t.id = NEW.type_id AND t.role = 'testCase')) THEN
        RAISE EXCEPTION 'only a requirement of a type marked as test cases has steps'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_test_case_fields';
    END IF;
    RETURN NEW;
END;
$$;

-- A type stays marked as test cases while any of its requirements, trashed ones too, holds what only a test case
-- holds.
CREATE FUNCTION requirement_type_role_held() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.role = 'testCase' AND NEW.role <> 'testCase' AND EXISTS (
        SELECT 1 FROM requirement r WHERE r.type_id = NEW.id AND requirement_holds_test(r.preconditions, r.steps, r.parameters)) THEN
        RAISE EXCEPTION 'the type''s requirements hold steps'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_type_role_held';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_test_case_fields BEFORE INSERT OR UPDATE OF kind, type_id, preconditions, steps, parameters
    ON requirement FOR EACH ROW EXECUTE FUNCTION requirement_test_case_fields();
CREATE TRIGGER requirement_type_role_held BEFORE UPDATE OF role ON requirement_type
    FOR EACH ROW EXECUTE FUNCTION requirement_type_role_held();

GRANT UPDATE (preconditions, steps, parameters) ON requirement TO exciter_app;

-- A file a test case's preconditions or steps show is kept, as one its statement shows.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attachment_referenced(requirement uuid, attachment uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM requirement_revision rv
        WHERE rv.requirement_id = attachment_referenced.requirement
          AND jsonb_path_exists(jsonb_build_array(rv.statement, rv.rationale, rv.attributes, rv.preconditions, rv.steps),
                'lax $.** ? ((@.type == "image" || @.type == "attachment") && @.attrs.attachmentId == $id)',
                jsonb_build_object('id', attachment_referenced.attachment::text)))
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attachment_referenced(requirement uuid, attachment uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM requirement_revision rv
        WHERE rv.requirement_id = attachment_referenced.requirement
          AND jsonb_path_exists(jsonb_build_array(rv.statement, rv.rationale, rv.attributes),
                'lax $.** ? ((@.type == "image" || @.type == "attachment") && @.attrs.attachmentId == $id)',
                jsonb_build_object('id', attachment_referenced.attachment::text)))
$$;
-- +goose StatementEnd
DROP TRIGGER requirement_type_role_held ON requirement_type;
DROP TRIGGER requirement_test_case_fields ON requirement;
DROP FUNCTION requirement_type_role_held();
DROP FUNCTION requirement_test_case_fields();
DROP FUNCTION requirement_holds_test(jsonb, jsonb, jsonb);
ALTER TABLE requirement_revision DROP COLUMN parameters, DROP COLUMN steps, DROP COLUMN preconditions;
ALTER TABLE requirement DROP COLUMN parameters, DROP COLUMN steps, DROP COLUMN preconditions;
ALTER TABLE requirement_type DROP COLUMN role;
