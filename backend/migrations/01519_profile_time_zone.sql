-- +goose Up
-- The time zone a person's dates and times are written in (#474), chosen on
-- the profile beside the language. The column came with the first migration,
-- defaulted to UTC and was never offered to anybody, so every stored value is
-- that default and none is a choice: they become null, which means following
-- the browser. A choice is a zone the database knows, whatever writes it.
ALTER TABLE app_user ALTER COLUMN timezone DROP NOT NULL, ALTER COLUMN timezone DROP DEFAULT;
-- Not forced meanwhile, so that an owner that is no superuser reaches every row too.
ALTER TABLE app_user NO FORCE ROW LEVEL SECURITY;
UPDATE app_user SET timezone = NULL;
ALTER TABLE app_user FORCE ROW LEVEL SECURITY;

-- Converting an instant into the zone fails for a name the database does not
-- know, so the check refuses it rather than storing it.
ALTER TABLE app_user ADD CONSTRAINT app_user_timezone_known
    CHECK (timezone IS NULL OR (timestamptz '2000-01-01 00:00:00+00' AT TIME ZONE timezone) IS NOT NULL);

-- +goose Down
ALTER TABLE app_user DROP CONSTRAINT IF EXISTS app_user_timezone_known;
ALTER TABLE app_user NO FORCE ROW LEVEL SECURITY;
UPDATE app_user SET timezone = 'UTC' WHERE timezone IS NULL;
ALTER TABLE app_user FORCE ROW LEVEL SECURITY;
ALTER TABLE app_user ALTER COLUMN timezone SET DEFAULT 'UTC', ALTER COLUMN timezone SET NOT NULL;
