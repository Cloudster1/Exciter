-- +goose Up
-- A narrower token for one run (#235): a client trades an access token of its own grant for a shorter one that may
-- only read, or reach fewer projects, while the grant keeps what its person allowed (RFC 8693).

-- Any client may register for the exchange beside the code it connects through; a partner's grants stay its own.
ALTER TABLE oauth_client DROP CONSTRAINT oauth_client_grant_types_check,
    ADD CONSTRAINT oauth_client_grant_types_check CHECK (
        CASE WHEN kind = 'partner'
             THEN grant_types = ARRAY['urn:ietf:params:oauth:grant-type:token-exchange', 'refresh_token']::text[]
             ELSE grant_types <@ ARRAY['authorization_code', 'refresh_token',
                                       'urn:ietf:params:oauth:grant-type:token-exchange']::text[] END);

-- An exchanged token is an access token and never the subject of another exchange; only it has projects of its own.
ALTER TABLE oauth_token ADD COLUMN exchanged boolean NOT NULL DEFAULT false,
    ADD COLUMN projects_only boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT oauth_token_exchanged_access CHECK (NOT exchanged OR kind = 'access'),
    ADD CONSTRAINT oauth_token_projects_exchanged CHECK (NOT projects_only OR exchanged);

-- The projects a narrower token reaches, each within its grant's when it was made; it ends with the token.
CREATE TABLE oauth_token_project (
    token_hash bytea NOT NULL REFERENCES oauth_token(token_hash) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    PRIMARY KEY (token_hash, project_id)
);

CREATE INDEX oauth_token_project_project_idx ON oauth_token_project (project_id);

ALTER TABLE oauth_token_project ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_token_project FORCE  ROW LEVEL SECURITY;
CREATE POLICY oauth_token_project_installation ON oauth_token_project USING (true) WITH CHECK (true);

-- Only the token endpoint and authentication touch it, as the admin role, as they do the tokens.
REVOKE ALL ON oauth_token_project FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON oauth_token_project TO exciter_admin;

-- +goose Down
DROP TABLE IF EXISTS oauth_token_project;
DELETE FROM oauth_token WHERE exchanged;
ALTER TABLE oauth_token DROP CONSTRAINT IF EXISTS oauth_token_projects_exchanged,
    DROP CONSTRAINT IF EXISTS oauth_token_exchanged_access,
    DROP COLUMN IF EXISTS projects_only,
    DROP COLUMN IF EXISTS exchanged;
UPDATE oauth_client SET grant_types = array_remove(grant_types, 'urn:ietf:params:oauth:grant-type:token-exchange')
    WHERE kind <> 'partner';
ALTER TABLE oauth_client DROP CONSTRAINT oauth_client_grant_types_check,
    ADD CONSTRAINT oauth_client_grant_types_check CHECK (
        CASE WHEN kind = 'partner'
             THEN grant_types = ARRAY['urn:ietf:params:oauth:grant-type:token-exchange', 'refresh_token']::text[]
             ELSE grant_types <@ ARRAY['authorization_code', 'refresh_token']::text[] END);
