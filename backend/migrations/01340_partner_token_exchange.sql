-- +goose Up
-- Connecting people to Armature and Stator at sign-in (#274): with a shared identity provider, Exciter exchanges
-- the person's access token at the partner's token endpoint (RFC 8693) as the partner app an administrator
-- registered there, whose client id and secret the connection keeps, the secret sealed with EXCITER_SECRET_KEY.
ALTER TABLE armature_connection ADD COLUMN client_id text, ADD COLUMN client_secret bytea;
ALTER TABLE stator_connection ADD COLUMN client_id text, ADD COLUMN client_secret bytea,
    -- Whether Stator lists oauth.token-exchange@1, read from its /meta as remote_links is, by the admin role.
    ADD COLUMN token_exchange boolean NOT NULL DEFAULT false;
GRANT INSERT (client_id, client_secret), UPDATE (client_id, client_secret) ON armature_connection, stator_connection TO exciter_app;

-- A token from the exchange sits where a pasted one does, so every read and the link worker use it alike. It
-- comes with a refresh token, sealed like it, and an hour to live; source tells it from a token the person
-- pasted, which a sign-in never replaces while it works. Only the service writes these, as the admin role.
ALTER TABLE armature_token
    ADD COLUMN source text NOT NULL DEFAULT 'token' CHECK (source IN ('token', 'exchange')),
    ADD COLUMN refresh_token bytea,
    ADD COLUMN access_expires_at timestamptz,
    ADD CONSTRAINT armature_token_exchange_refreshes CHECK (source = 'token' OR (refresh_token IS NOT NULL AND access_expires_at IS NOT NULL));
ALTER TABLE stator_token
    ADD COLUMN source text NOT NULL DEFAULT 'token' CHECK (source IN ('token', 'exchange')),
    ADD COLUMN refresh_token bytea,
    ADD COLUMN access_expires_at timestamptz,
    ADD CONSTRAINT stator_token_exchange_refreshes CHECK (source = 'token' OR (refresh_token IS NOT NULL AND access_expires_at IS NOT NULL));

-- What the last sign-in's exchange came to, per person and partner, so the account page says in a sentence why
-- somebody is not connected. Written by the service as the admin role; each person reads their own.
CREATE TABLE partner_exchange (
    partner   text NOT NULL CHECK (partner IN ('armature', 'stator')),
    user_id   uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    connected boolean NOT NULL,
    message   text NOT NULL DEFAULT '',
    at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (partner, user_id)
);
ALTER TABLE partner_exchange ENABLE ROW LEVEL SECURITY;
ALTER TABLE partner_exchange FORCE  ROW LEVEL SECURITY;
CREATE POLICY partner_exchange_installation ON partner_exchange USING (true) WITH CHECK (true);
CREATE POLICY partner_exchange_own ON partner_exchange AS RESTRICTIVE FOR ALL TO exciter_app
    USING (user_id = current_actor_id()) WITH CHECK (user_id = current_actor_id());
GRANT SELECT, INSERT, UPDATE, DELETE ON partner_exchange TO exciter_admin;
REVOKE ALL ON partner_exchange FROM exciter_app;
GRANT SELECT ON partner_exchange TO exciter_app;

-- +goose Down
DROP TABLE partner_exchange;
ALTER TABLE stator_token DROP CONSTRAINT stator_token_exchange_refreshes,
    DROP COLUMN access_expires_at, DROP COLUMN refresh_token, DROP COLUMN source;
ALTER TABLE armature_token DROP CONSTRAINT armature_token_exchange_refreshes,
    DROP COLUMN access_expires_at, DROP COLUMN refresh_token, DROP COLUMN source;
ALTER TABLE stator_connection DROP COLUMN token_exchange, DROP COLUMN client_secret, DROP COLUMN client_id;
ALTER TABLE armature_connection DROP COLUMN client_secret, DROP COLUMN client_id;
