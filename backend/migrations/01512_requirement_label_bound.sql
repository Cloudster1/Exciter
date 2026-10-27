-- +goose Up
-- A requirement carries at most label.MaxPerRequirement labels (#309). The service held the bound alone, so a
-- writer straight through SQL put on a 51st; the database now holds it too, as it does a requirement's links.

-- +goose StatementBegin
CREATE FUNCTION requirement_label_bound() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    -- label.MaxPerRequirement.
    IF (SELECT count(*) FROM requirement_label WHERE requirement_id = NEW.requirement_id) >= 50 THEN
        RAISE EXCEPTION 'a requirement carries at most 50 labels'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_label_bound';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_label_bound BEFORE INSERT ON requirement_label
    FOR EACH ROW EXECUTE FUNCTION requirement_label_bound();

-- +goose Down
DROP TRIGGER IF EXISTS requirement_label_bound ON requirement_label;
DROP FUNCTION IF EXISTS requirement_label_bound();
