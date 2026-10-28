-- +goose Up
-- A mitigation joins a requirement that is no risk to a risk (#325). trace_link_mitigates_risk held that only when
-- the link was written, so a risk moved to a type of requirements, or its mitigating requirement moved to a type
-- of risks, broke it afterwards. Either end now keeps its role while a live mitigation joins it.

-- +goose StatementBegin
CREATE FUNCTION requirement_mitigation_held() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    risky boolean := NEW.kind = 'requirement' AND EXISTS (
        SELECT 1 FROM requirement_type t WHERE t.id = NEW.type_id AND t.role = 'risk');
BEGIN
    IF EXISTS (SELECT 1 FROM trace_link l JOIN link_type lt ON lt.id = l.type_id AND lt.counts = 'mitigation'
               WHERE l.deleted_at IS NULL
                 AND ((l.target_id = NEW.id AND NOT risky) OR (l.source_id = NEW.id AND risky))) THEN
        RAISE EXCEPTION 'a mitigation joins the requirement to a risk'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_mitigation_held';
    END IF;
    RETURN NEW;
END;
$$;

-- A type becomes one of risks only while none of its requirements mitigates a risk; turning back from risks is held
-- by requirement_type_role_holds already.
CREATE OR REPLACE FUNCTION requirement_type_role_held() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.role <> NEW.role AND (requirement_type_role_holds(NEW.id) OR (NEW.role = 'risk' AND EXISTS (
        SELECT 1 FROM requirement r
        JOIN trace_link l ON l.source_id = r.id AND l.deleted_at IS NULL
        JOIN link_type lt ON lt.id = l.type_id AND lt.counts = 'mitigation'
        WHERE r.type_id = NEW.id))) THEN
        RAISE EXCEPTION 'the type''s requirements hold what only its role holds'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_type_role_held';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_mitigation_held BEFORE UPDATE OF kind, type_id ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_mitigation_held();

-- +goose Down
DROP TRIGGER requirement_mitigation_held ON requirement;
DROP FUNCTION requirement_mitigation_held();
-- +goose StatementBegin
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
-- +goose StatementEnd
