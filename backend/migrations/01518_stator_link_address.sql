-- +goose Up
-- A page link opens under the connected Stator's address (#327). The service always writes it there, but the trigger
-- asked only for a connection, so a writer straight through SQL pointed a link that everyone clicks anywhere at all.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION stator_page_link_open() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM stator_connection) THEN
        RAISE EXCEPTION 'Exciter is not connected to Stator' USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM stator_connection c WHERE starts_with(NEW.url, rtrim(c.base_url, '/') || '/s/')) THEN
        RAISE EXCEPTION 'a Stator page link opens under the connected Stator''s address' USING ERRCODE = 'check_violation';
    END IF;
    SELECT revision INTO NEW.reviewed_rev FROM requirement
    WHERE id = NEW.requirement_id AND kind = 'requirement' AND trashed_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'only a requirement out of the trash takes Stator pages' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION stator_page_link_open() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM stator_connection) THEN
        RAISE EXCEPTION 'Exciter is not connected to Stator' USING ERRCODE = 'check_violation';
    END IF;
    SELECT revision INTO NEW.reviewed_rev FROM requirement
    WHERE id = NEW.requirement_id AND kind = 'requirement' AND trashed_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'only a requirement out of the trash takes Stator pages' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
