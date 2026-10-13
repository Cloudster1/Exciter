-- +goose Up
-- An installation is one team's requirements, with one organization in it (#84). A database holding several is
-- refused rather than merged: their project keys, statuses, workflows and people would collide. No release has
-- a way to delete one, so the refusal says the statement. Nothing here changes the schema.
--
-- An owner that is not a superuser sees no row of org while its row security is forced, so it is lifted to count
-- them; a refusal rolls the lift back with it.
ALTER TABLE org NO FORCE ROW LEVEL SECURITY;

-- +goose StatementBegin
DO $$
DECLARE
    listed CONSTANT int := 10;
    total int;
    names text;
BEGIN
    SELECT count(*) INTO total FROM org;
    IF total <= 1 THEN
        RETURN;
    END IF;
    SELECT string_agg(format('%s (%s)', slug, name), ', ' ORDER BY created_at, slug) INTO names
    FROM (SELECT slug, name, created_at FROM org ORDER BY created_at, slug LIMIT listed) first_ones;
    IF total > listed THEN
        names := names || format(' and %s more', total - listed);
    END IF;
    RAISE EXCEPTION 'This database holds % organizations, and Exciter now runs one per installation: %. Keep the one to go on with, delete the others with DELETE FROM org WHERE slug = ..., which removes everything in them, then migrate again.', total, names
        USING ERRCODE = 'check_violation';
END;
$$;
-- +goose StatementEnd

ALTER TABLE org FORCE ROW LEVEL SECURITY;

-- +goose Down
-- The guard changed nothing, so there is nothing to take back.
