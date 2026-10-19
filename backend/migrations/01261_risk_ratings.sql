-- +goose Up
-- Risks and their ratings (#259), as docs/api-contract-m13.md agrees: a requirement type may be marked as risks, and
-- a requirement of such a type is rated before and after mitigation, as numbers on its project's scales, in every
-- revision.

ALTER TABLE requirement_type DROP CONSTRAINT requirement_type_role_check;
ALTER TABLE requirement_type ADD CONSTRAINT requirement_type_role_check CHECK (role IN ('requirement', 'testCase', 'risk'));

-- {"before": {"severity": 4, "probability": 2, "detection": null}, "after": {...}}; null for none.
ALTER TABLE requirement ADD COLUMN risk jsonb CHECK (risk IS NULL OR jsonb_typeof(risk) = 'object');
ALTER TABLE requirement_revision ADD COLUMN risk jsonb CHECK (risk IS NULL OR jsonb_typeof(risk) = 'object');
CREATE INDEX requirement_risk_idx ON requirement (project_id) WHERE risk IS NOT NULL;
CREATE INDEX requirement_revision_risk_idx ON requirement_revision (requirement_id) WHERE risk IS NOT NULL;

-- +goose StatementBegin
-- Whether a rating has the shape it must and only values its project's scales hold: detection only when the scheme
-- rates it. A project without a scheme takes no rating.
CREATE FUNCTION risk_rating_fits(target_project uuid, rating jsonb) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM risk_scheme s WHERE s.project_id = target_project)
       AND NOT EXISTS (SELECT 1 FROM jsonb_each(rating) side
                       WHERE side.key NOT IN ('before', 'after') OR jsonb_typeof(side.value) <> 'object')
       AND NOT EXISTS (
           SELECT 1 FROM jsonb_each(rating) side, jsonb_each(side.value) v
           WHERE jsonb_typeof(v.value) <> 'null'
             AND (v.key NOT IN ('severity', 'probability', 'detection')
                  OR jsonb_typeof(v.value) <> 'number'
                  OR NOT EXISTS (
                      SELECT 1 FROM risk_scale_level l JOIN risk_scheme s ON s.project_id = l.project_id
                      WHERE l.project_id = target_project AND l.dimension = v.key AND l.value::numeric = (v.value #>> '{}')::numeric
                        AND (v.key <> 'detection' OR s.detection))))
$$;

-- Whether a rating gives a dimension a value, on either side.
CREATE FUNCTION risk_rating_uses(rating jsonb, dimension text, value integer) RETURNS boolean
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT (rating->'before'->>dimension)::integer = value OR (rating->'after'->>dimension)::integer = value
$$;

-- Whether a project's requirements, trashed ones and every revision included, rate a dimension, at one value or any.
CREATE FUNCTION risk_scale_used(target_project uuid, dimension text, value integer) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
               SELECT 1 FROM requirement r
               WHERE r.project_id = target_project AND r.risk IS NOT NULL
                 AND CASE WHEN value IS NULL THEN COALESCE(r.risk->'before'->>dimension, r.risk->'after'->>dimension) IS NOT NULL
                          ELSE COALESCE(risk_rating_uses(r.risk, dimension, value), false) END)
        OR EXISTS (
               SELECT 1 FROM requirement r JOIN requirement_revision v ON v.requirement_id = r.id
               WHERE r.project_id = target_project AND v.risk IS NOT NULL
                 AND CASE WHEN value IS NULL THEN COALESCE(v.risk->'before'->>dimension, v.risk->'after'->>dimension) IS NOT NULL
                          ELSE COALESCE(risk_rating_uses(v.risk, dimension, value), false) END)
$$;

-- A rating belongs to a requirement, never a section, of a type marked as risks, and holds its project's values. A
-- revision copies its requirement's row, so holding the row holds every revision written from it.
CREATE FUNCTION requirement_risk_fields() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.risk IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.kind <> 'requirement' OR NOT EXISTS (
        SELECT 1 FROM requirement_type t WHERE t.id = NEW.type_id AND t.role = 'risk') THEN
        RAISE EXCEPTION 'only a requirement of a type marked as risks is rated'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_risk_fields';
    END IF;
    IF NOT risk_rating_fits(NEW.project_id, NEW.risk) THEN
        RAISE EXCEPTION 'the rating holds a value the project''s scales lack'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_risk_scale';
    END IF;
    RETURN NEW;
END;
$$;

-- A scale keeps a value while a rating uses it, so no revision ever names a value its project lost. A scheme
-- deleted with its project lets go.
CREATE FUNCTION risk_scale_level_held() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF pg_trigger_depth() = 1 AND risk_scale_used(OLD.project_id, OLD.dimension, OLD.value) THEN
        RAISE EXCEPTION 'a rating uses % %', OLD.dimension, OLD.value
            USING ERRCODE = 'check_violation', CONSTRAINT = 'risk_scale_held';
    END IF;
    RETURN OLD;
END;
$$;

-- A scheme keeps rating detection while a rating gives it.
CREATE FUNCTION risk_scheme_detection_held() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.detection AND NOT NEW.detection AND risk_scale_used(NEW.project_id, 'detection', NULL) THEN
        RAISE EXCEPTION 'a rating gives detection'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'risk_scale_held';
    END IF;
    RETURN NEW;
END;
$$;

-- A type keeps its role while any of its requirements, trashed ones too, holds what only that role holds: steps
-- for test cases (#250), a rating for risks.
CREATE OR REPLACE FUNCTION requirement_type_role_held() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.role <> NEW.role AND requirement_type_role_holds(NEW.id) THEN
        RAISE EXCEPTION 'the type''s requirements hold what only its role holds'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_type_role_held';
    END IF;
    RETURN NEW;
END;
$$;

-- Whether any requirement of a type, trashed ones too, holds what only the type's role gives.
CREATE FUNCTION requirement_type_role_holds(target_type uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM requirement r
        WHERE r.type_id = target_type
          AND (requirement_holds_test(r.preconditions, r.steps, r.parameters) OR r.risk IS NOT NULL))
$$;

-- A rating as it is read: each side's priority number and level under the scheme. Until mitigations (#260) a risk
-- is unmitigated and its level is the one before; a revision, read without its requirement, has no state.
CREATE FUNCTION risk_view(target_project uuid, rating jsonb, target uuid) RETURNS jsonb
    LANGUAGE sql STABLE
AS $$
    SELECT jsonb_build_object(
        'beforeRpn', risk_rpn(rating->'before', s.detection),
        'afterRpn', risk_rpn(rating->'after', s.detection),
        'beforeLevel', risk_level_ref(risk_level_of(target_project, rating->'before')),
        'afterLevel', risk_level_ref(risk_level_of(target_project, rating->'after')),
        'state', CASE WHEN target IS NOT NULL THEN 'unmitigated' END,
        'level', CASE WHEN target IS NOT NULL THEN risk_level_ref(risk_level_of(target_project, rating->'before')) END)
    FROM (SELECT (SELECT sc.detection FROM risk_scheme sc WHERE sc.project_id = target_project) AS detection) s
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_risk_fields BEFORE INSERT OR UPDATE OF kind, type_id, project_id, risk
    ON requirement FOR EACH ROW EXECUTE FUNCTION requirement_risk_fields();
CREATE TRIGGER risk_scale_level_held BEFORE DELETE ON risk_scale_level
    FOR EACH ROW EXECUTE FUNCTION risk_scale_level_held();
CREATE TRIGGER risk_scheme_detection_held BEFORE UPDATE OF detection ON risk_scheme
    FOR EACH ROW EXECUTE FUNCTION risk_scheme_detection_held();

GRANT UPDATE (risk) ON requirement TO exciter_app;
REVOKE ALL ON FUNCTION risk_rating_fits(uuid, jsonb), risk_scale_used(uuid, text, integer), requirement_type_role_holds(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION risk_rating_fits(uuid, jsonb), risk_scale_used(uuid, text, integer), requirement_type_role_holds(uuid)
    TO exciter_app, exciter_admin;

-- +goose Down
DROP TRIGGER risk_scheme_detection_held ON risk_scheme;
DROP TRIGGER risk_scale_level_held ON risk_scale_level;
DROP TRIGGER requirement_risk_fields ON requirement;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION requirement_type_role_held() RETURNS trigger
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
DROP FUNCTION risk_view(uuid, jsonb, uuid);
DROP FUNCTION requirement_type_role_holds(uuid);
DROP FUNCTION risk_scheme_detection_held();
DROP FUNCTION risk_scale_level_held();
DROP FUNCTION requirement_risk_fields();
DROP FUNCTION risk_scale_used(uuid, text, integer);
DROP FUNCTION risk_rating_uses(jsonb, text, integer);
DROP FUNCTION risk_rating_fits(uuid, jsonb);
DROP INDEX requirement_revision_risk_idx;
DROP INDEX requirement_risk_idx;
ALTER TABLE requirement_revision DROP COLUMN risk;
ALTER TABLE requirement DROP COLUMN risk;
UPDATE requirement_type SET role = 'requirement' WHERE role = 'risk';
ALTER TABLE requirement_type DROP CONSTRAINT requirement_type_role_check;
ALTER TABLE requirement_type ADD CONSTRAINT requirement_type_role_check CHECK (role IN ('requirement', 'testCase'));
