-- +goose Up
-- A row needs nobody to say which organization it is of (#106): every org_id column defaults to the installation's
-- one row, so the functions of 00413, and the code after them (#107), write rows without it until #108 drops the
-- column. A fresh database migrates with no row yet, so the default looks the row up when a row is written.

-- +goose StatementBegin
CREATE FUNCTION installation_org_id() RETURNS uuid
    LANGUAGE plpgsql STABLE
AS $$
DECLARE
    found uuid[];
BEGIN
    SELECT array_agg(id) INTO found FROM org;
    IF cardinality(found) = 1 THEN
        RETURN found[1];
    END IF;
    IF found IS NULL THEN
        RAISE EXCEPTION 'This database has no installation yet. Start Exciter once, so its first start makes it, then try again.'
            USING ERRCODE = 'not_null_violation';
    END IF;
    RAISE EXCEPTION 'This database holds % organizations, and Exciter runs one per installation, so a row cannot tell which it belongs to. Delete the extra ones, then try again.', cardinality(found)
        USING ERRCODE = 'cardinality_violation';
END;
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION installation_org_id() TO exciter_app, exciter_admin;

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOR t IN SELECT table_name FROM information_schema.columns
             WHERE table_schema = 'public' AND column_name = 'org_id' LOOP
        EXECUTE format('ALTER TABLE %I ALTER COLUMN org_id SET DEFAULT installation_org_id()', t);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOR t IN SELECT table_name FROM information_schema.columns
             WHERE table_schema = 'public' AND column_name = 'org_id' LOOP
        EXECUTE format('ALTER TABLE %I ALTER COLUMN org_id DROP DEFAULT', t);
    END LOOP;
END $$;
-- +goose StatementEnd
DROP FUNCTION installation_org_id();
