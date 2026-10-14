-- +goose Up
-- The code no longer names the organization (#107), so nothing sets app.org_id, and current_org_id() read from it
-- would answer NULL and match nothing. Until #108 drops it, it answers the installation's one row instead, so what
-- still calls it, such as a script or a query of an operator's, keeps working.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_org_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT (SELECT id FROM org)
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_org_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.org_id', true), '')::uuid
$$;
-- +goose StatementEnd
