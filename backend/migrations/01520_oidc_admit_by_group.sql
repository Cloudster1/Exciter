-- +goose Up
-- Somebody new in a group mapped to a role is let in at sign-in or at a
-- partner's token exchange only once an administrator turns this on, as in
-- Armature and Stator (#469). An installation that already maps a group let
-- such people in from the moment it was mapped, so it keeps doing so.
ALTER TABLE oidc_provider ADD COLUMN admit_by_group boolean NOT NULL DEFAULT false;
-- Not forced meanwhile, so that an owner that is no superuser reaches every row too.
ALTER TABLE oidc_provider NO FORCE ROW LEVEL SECURITY;
ALTER TABLE oidc_group_role NO FORCE ROW LEVEL SECURITY;
UPDATE oidc_provider SET admit_by_group = true WHERE EXISTS (SELECT 1 FROM oidc_group_role);
ALTER TABLE oidc_group_role FORCE ROW LEVEL SECURITY;
ALTER TABLE oidc_provider FORCE ROW LEVEL SECURITY;

-- +goose Down
ALTER TABLE oidc_provider DROP COLUMN IF EXISTS admit_by_group;
