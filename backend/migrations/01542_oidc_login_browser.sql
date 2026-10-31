-- +goose Up
-- A sign-in finishes only in the browser that started it: the digest of a secret kept in that browser's cookie,
-- never the secret itself. Sign-ins started before this cannot finish anyway, since their browsers hold no cookie.
-- Not forced meanwhile, so that an owner that is no superuser reaches every row too.
ALTER TABLE oidc_login NO FORCE ROW LEVEL SECURITY;
DELETE FROM oidc_login;
ALTER TABLE oidc_login FORCE ROW LEVEL SECURITY;
ALTER TABLE oidc_login ADD COLUMN browser_hash bytea NOT NULL CHECK (octet_length(browser_hash) = 32);

-- +goose Down
ALTER TABLE oidc_login DROP COLUMN IF EXISTS browser_hash;
