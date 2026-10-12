-- +goose Up
-- Foundation: organizations, people and membership, the two runtime roles, and
-- the row level security machinery every later tenant table plugs into.

-- IDs come from uuidv7(), which Postgres has built in from version 18 on.
-- +goose StatementBegin
DO $$
BEGIN
    IF current_setting('server_version_num')::int < 180000 THEN
        RAISE EXCEPTION 'Exciter needs PostgreSQL 18 or newer, and this server runs %. Upgrade it and migrate again.',
            current_setting('server_version');
    END IF;
END;
$$;
-- +goose StatementEnd

CREATE EXTENSION IF NOT EXISTS citext;

-- exciter_app is subject to every policy; exciter_admin is exempt by policy rather
-- than by being a superuser, so the exemption is visible in the schema. A
-- deployment that created them beforehand, with a password to log in, keeps them.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'exciter_app') THEN
        CREATE ROLE exciter_app NOLOGIN NOSUPERUSER NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'exciter_admin') THEN
        CREATE ROLE exciter_admin NOLOGIN NOSUPERUSER NOBYPASSRLS;
    END IF;
END;
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA public TO exciter_app, exciter_admin;

-- Everything later migrations create is granted to the runtime roles as it is
-- made, so a new table is never accidentally unreachable.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO exciter_app, exciter_admin;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO exciter_app, exciter_admin;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT EXECUTE ON FUNCTIONS TO exciter_app, exciter_admin;

-- current_org_id is NULL when the transaction names no tenant, which makes every
-- policy fail closed: an unscoped connection sees no tenant rows, not all of them.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_org_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.org_id', true), '')::uuid
$$;
-- +goose StatementEnd

-- The person the transaction acts for, set with app.org_id. NULL binds the
-- app role to nothing at all, as a missing organization does.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_actor_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.user_id', true), '')::uuid
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TABLE org (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    slug        citext NOT NULL UNIQUE,
    name        text NOT NULL,
    settings    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT org_slug_shape CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$')
);

CREATE TRIGGER org_set_updated_at BEFORE UPDATE ON org
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A person is global, not per tenant: one account can belong to several
-- organizations, so app_user carries no org_id and is scoped through org_member.
CREATE TABLE app_user (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    email      citext NOT NULL UNIQUE,
    name       text NOT NULL,
    avatar_url text,
    timezone   text NOT NULL DEFAULT 'UTC',
    locale     text NOT NULL DEFAULT 'en',
    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT app_user_email_shape CHECK (position('@' in email) > 1)
);

CREATE TRIGGER app_user_set_updated_at BEFORE UPDATE ON app_user
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE org_member (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    org_role   text NOT NULL CHECK (org_role IN ('owner', 'admin', 'member')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);

CREATE INDEX org_member_user_idx ON org_member (user_id);

CREATE TRIGGER org_member_set_updated_at BEFORE UPDATE ON org_member
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- FORCE, not just ENABLE: without it the table owner bypasses its own policies,
-- and migrations plus tests routinely connect as the owner.
ALTER TABLE org        ENABLE ROW LEVEL SECURITY;
ALTER TABLE org        FORCE  ROW LEVEL SECURITY;
ALTER TABLE app_user   ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_user   FORCE  ROW LEVEL SECURITY;
ALTER TABLE org_member ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_member FORCE  ROW LEVEL SECURITY;

CREATE POLICY org_tenant_isolation ON org
    USING (id = current_org_id())
    WITH CHECK (id = current_org_id());

CREATE POLICY org_member_tenant_isolation ON org_member
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

-- A tenant sees the people who are its members and nobody else; making a person
-- is signup's job, which runs as exciter_admin before any membership exists.
CREATE POLICY app_user_tenant_isolation ON app_user
    USING (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id AND m.org_id = current_org_id()))
    WITH CHECK (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id AND m.org_id = current_org_id()));

CREATE POLICY org_admin_bypass ON org TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY app_user_admin_bypass ON app_user TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY org_member_admin_bypass ON org_member TO exciter_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON org, app_user, org_member TO exciter_app, exciter_admin;

-- +goose Down
-- The roles and default privileges stay: roles belong to the whole server, and
-- another database on it may use them.
DROP TABLE IF EXISTS org_member, app_user, org CASCADE;
DROP FUNCTION IF EXISTS set_updated_at();
DROP FUNCTION IF EXISTS current_actor_id();
DROP FUNCTION IF EXISTS current_org_id();
