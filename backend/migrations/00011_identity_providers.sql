-- +goose Up
-- Signing in through an organization's identity provider: the provider itself, the sign-ins in flight, and which
-- provider identity belongs to which person.

-- One provider per organization. The secret is kept because the code exchange is a call the server makes; it is
-- sealed with EXCITER_SECRET_KEY, bound to the organization, so a copy of the database alone does not reveal it.
CREATE TABLE oidc_provider (
    org_id        uuid PRIMARY KEY REFERENCES org(id) ON DELETE CASCADE,
    issuer        text NOT NULL,
    client_id     text NOT NULL,
    client_secret bytea,
    -- Providers disagree about the name of the claim listing somebody's groups, so it is configuration.
    groups_claim  text NOT NULL DEFAULT 'groups',
    scopes        text NOT NULL DEFAULT 'openid profile email',
    -- Off by default: a group appearing on its own is a surprise, and an empty group grants nothing anyway.
    create_groups boolean NOT NULL DEFAULT false,
    enabled       boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT oidc_provider_issuer_not_blank CHECK (btrim(issuer) <> ''),
    CONSTRAINT oidc_provider_client_id_not_blank CHECK (btrim(client_id) <> '')
);

CREATE TRIGGER oidc_provider_set_updated_at BEFORE UPDATE ON oidc_provider
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A sign-in in flight. The state ties the callback to the attempt, the nonce ties the id token to it, and the
-- verifier proves the code was asked for by whoever redeems it. Stored rather than signed into a cookie, so a
-- callback reaching another instance is still recognised.
CREATE TABLE oidc_login (
    state         text PRIMARY KEY,
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    nonce         text NOT NULL,
    code_verifier text NOT NULL,
    redirect      text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL
);

CREATE INDEX oidc_login_expiry_idx ON oidc_login (expires_at);

-- A provider's subject is its own stable name for a person, which survives an email change; an issuer and a
-- subject together name one person here.
CREATE TABLE user_identity (
    issuer        text NOT NULL,
    subject       text NOT NULL,
    user_id       uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issuer, subject)
);

CREATE INDEX user_identity_user_idx ON user_identity (user_id);

ALTER TABLE oidc_provider ENABLE ROW LEVEL SECURITY;
ALTER TABLE oidc_provider FORCE  ROW LEVEL SECURITY;
ALTER TABLE oidc_login    ENABLE ROW LEVEL SECURITY;
ALTER TABLE oidc_login    FORCE  ROW LEVEL SECURITY;
ALTER TABLE user_identity ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_identity FORCE  ROW LEVEL SECURITY;

CREATE POLICY oidc_provider_tenant_isolation ON oidc_provider
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY oidc_login_tenant_isolation ON oidc_login
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY oidc_provider_admin_bypass ON oidc_provider TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY oidc_login_admin_bypass ON oidc_login TO exciter_admin USING (true) WITH CHECK (true);
-- Identities span organizations and are read only while signing in, before any tenant is known, so only the
-- admin role reaches them.
CREATE POLICY user_identity_admin_bypass ON user_identity TO exciter_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON oidc_provider, oidc_login, user_identity TO exciter_app, exciter_admin;

-- +goose Down
DROP TABLE IF EXISTS user_identity, oidc_login, oidc_provider CASCADE;
