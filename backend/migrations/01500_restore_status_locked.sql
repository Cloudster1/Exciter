-- +goose Up
-- A requirement in the trash cannot be taken out of a locking status, so a status that locks after it went there
-- would keep it from coming back (#350). Its place and the trash may change while it is there, which is what the
-- restore writes; what it says stays locked. A hand lock still holds, though requirement_lock refuses one in the trash.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION requirement_row_unlocked() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    why text;
BEGIN
    IF OLD.trashed_at IS NOT NULL AND OLD.locked_at IS NULL
       AND (NEW.title, NEW.statement, NEW.rationale, NEW.type_id, NEW.attributes, NEW.project_id)
           IS NOT DISTINCT FROM (OLD.title, OLD.statement, OLD.rationale, OLD.type_id, OLD.attributes, OLD.project_id) THEN
        RETURN NEW;
    END IF;
    IF (NEW.title, NEW.statement, NEW.rationale, NEW.type_id, NEW.attributes, NEW.parent_id, NEW.rank, NEW.trashed_at, NEW.project_id)
       IS DISTINCT FROM (OLD.title, OLD.statement, OLD.rationale, OLD.type_id, OLD.attributes, OLD.parent_id, OLD.rank, OLD.trashed_at, OLD.project_id) THEN
        why := requirement_lock_sentence(OLD.id);
        IF why IS NOT NULL THEN
            RAISE EXCEPTION '%', why USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_locked';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION requirement_row_unlocked() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    why text;
BEGIN
    IF (NEW.title, NEW.statement, NEW.rationale, NEW.type_id, NEW.attributes, NEW.parent_id, NEW.rank, NEW.trashed_at, NEW.project_id)
       IS DISTINCT FROM (OLD.title, OLD.statement, OLD.rationale, OLD.type_id, OLD.attributes, OLD.parent_id, OLD.rank, OLD.trashed_at, OLD.project_id) THEN
        why := requirement_lock_sentence(OLD.id);
        IF why IS NOT NULL THEN
            RAISE EXCEPTION '%', why USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_locked';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
