-- +goose Up
-- Partner apps (#273): a tool beside Exciter, such as Stator, that an administrator registers with a secret and the
-- identity provider's client it signs people in with, so it trades a person's access token from the shared realm
-- for a grant here (RFC 8693) with no consent page. Its grants are the person's connected apps like any other.

ALTER TABLE oauth_client DROP CONSTRAINT oauth_client_kind_check,
    ADD CONSTRAINT oauth_client_kind_check CHECK (kind IN ('registered', 'metadata', 'partner')),
    DROP CONSTRAINT oauth_client_redirect_uris_check,
    -- A partner is never sent a browser, so it has nowhere to be sent back to.
    ADD CONSTRAINT oauth_client_redirect_uris_check CHECK (
        CASE WHEN kind = 'partner' THEN cardinality(redirect_uris) = 0 ELSE cardinality(redirect_uris) BETWEEN 1 AND 10 END),
    DROP CONSTRAINT oauth_client_grant_types_check,
    ADD CONSTRAINT oauth_client_grant_types_check CHECK (
        CASE WHEN kind = 'partner'
             THEN grant_types = ARRAY['urn:ietf:params:oauth:grant-type:token-exchange', 'refresh_token']::text[]
             ELSE grant_types <@ ARRAY['authorization_code', 'refresh_token']::text[] END),
    DROP CONSTRAINT oauth_client_documents_keep_no_secret,
    ADD CONSTRAINT oauth_client_documents_keep_no_secret CHECK (kind <> 'metadata' OR auth_method = 'none'),
    ADD CONSTRAINT oauth_client_partners_keep_a_secret CHECK (kind <> 'partner' OR auth_method <> 'none'),
    -- Empty: the app exchanges nothing until an administrator names the client, as in Armature.
    ADD COLUMN keycloak_client_id text NOT NULL DEFAULT '' CHECK (char_length(keycloak_client_id) <= 255),
    -- What a partner's grants may ever do: read, and with write every right their person has.
    ADD COLUMN scopes text[] NOT NULL DEFAULT '{}' CHECK (scopes <@ ARRAY['read', 'write']::text[]),
    ADD CONSTRAINT oauth_client_partners_read CHECK (kind <> 'partner' OR 'read' = ANY (scopes)),
    ADD CONSTRAINT oauth_client_only_partners_are_named CHECK (kind = 'partner' OR (keycloak_client_id = '' AND scopes = '{}'));

-- Only an administrator, with the whole of their rights, registers, changes and removes a partner, and nobody makes
-- a client of another kind as the app role: those come through the protocol, as the admin role.
CREATE POLICY oauth_client_partner_made_by_administrators ON oauth_client AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (kind = 'partner' AND perm_token_whole(current_actor_id()) AND current_actor_administers());
CREATE POLICY oauth_client_partner_changed_by_administrators ON oauth_client AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (kind = 'partner' AND perm_token_whole(current_actor_id()) AND current_actor_administers())
    WITH CHECK (kind = 'partner');
CREATE POLICY oauth_client_partner_removed_by_administrators ON oauth_client AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (kind = 'partner' AND perm_token_whole(current_actor_id()) AND current_actor_administers());

GRANT SELECT (keycloak_client_id, scopes, created_at) ON oauth_client TO exciter_app;
GRANT INSERT (id, kind, name, uri, redirect_uris, auth_method, secret_hash, grant_types, keycloak_client_id, scopes)
    ON oauth_client TO exciter_app;
GRANT UPDATE (name, uri, secret_hash, keycloak_client_id, scopes) ON oauth_client TO exciter_app;
GRANT DELETE ON oauth_client TO exciter_app;

-- +goose Down
DELETE FROM oauth_client WHERE kind = 'partner';
REVOKE DELETE ON oauth_client FROM exciter_app;
REVOKE UPDATE (name, uri, secret_hash, keycloak_client_id, scopes) ON oauth_client FROM exciter_app;
REVOKE INSERT (id, kind, name, uri, redirect_uris, auth_method, secret_hash, grant_types, keycloak_client_id, scopes)
    ON oauth_client FROM exciter_app;
REVOKE SELECT (keycloak_client_id, scopes, created_at) ON oauth_client FROM exciter_app;
DROP POLICY IF EXISTS oauth_client_partner_removed_by_administrators ON oauth_client;
DROP POLICY IF EXISTS oauth_client_partner_changed_by_administrators ON oauth_client;
DROP POLICY IF EXISTS oauth_client_partner_made_by_administrators ON oauth_client;
ALTER TABLE oauth_client DROP CONSTRAINT IF EXISTS oauth_client_only_partners_are_named,
    DROP CONSTRAINT IF EXISTS oauth_client_partners_read,
    DROP COLUMN IF EXISTS scopes,
    DROP COLUMN IF EXISTS keycloak_client_id,
    DROP CONSTRAINT IF EXISTS oauth_client_partners_keep_a_secret,
    DROP CONSTRAINT oauth_client_documents_keep_no_secret,
    ADD CONSTRAINT oauth_client_documents_keep_no_secret CHECK (kind = 'registered' OR auth_method = 'none'),
    DROP CONSTRAINT oauth_client_grant_types_check,
    ADD CONSTRAINT oauth_client_grant_types_check CHECK (grant_types <@ ARRAY['authorization_code', 'refresh_token']::text[]),
    DROP CONSTRAINT oauth_client_redirect_uris_check,
    ADD CONSTRAINT oauth_client_redirect_uris_check CHECK (cardinality(redirect_uris) BETWEEN 1 AND 10),
    DROP CONSTRAINT oauth_client_kind_check,
    ADD CONSTRAINT oauth_client_kind_check CHECK (kind IN ('registered', 'metadata'));
