-- +goose Up
-- The Armature instance an organization connects (#26), and each member's token for it, as Stator keeps them.
-- One connection per organization: where people open Armature, the Armature organization every stored token has
-- to belong to, and the secret Armature signs webhooks with, sealed with EXCITER_SECRET_KEY.
CREATE TABLE armature_connection (
    org_id          uuid PRIMARY KEY REFERENCES org(id) ON DELETE CASCADE,
    -- An origin: a scheme, a host and maybe a port, nothing after it. The service says why in words; this keeps
    -- raw SQL to the same shape.
    base_url        text NOT NULL,
    org_slug        text NOT NULL,
    -- Learned from the first token that checks out, by the service as the admin role, so nobody pins it with SQL.
    armature_org_id uuid,
    webhook_secret  bytea,
    updated_by      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT armature_connection_base_url_origin CHECK (base_url ~ '^https?://[^/?#@[:space:]]+$'),
    CONSTRAINT armature_connection_org_slug_shape CHECK (org_slug ~ '^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$')
);

CREATE TRIGGER armature_connection_set_updated_at BEFORE UPDATE ON armature_connection
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A member's Armature personal access token, sealed and bound to the organization and the person. Every call to
-- Armature is made with the viewer's own, so Exciter shows an issue only to somebody Armature shows it to.
CREATE TABLE armature_token (
    org_id              uuid NOT NULL,
    user_id             uuid NOT NULL,
    -- New with every token stored, so a cache entry keyed by it is never read for another token.
    id                  uuid NOT NULL DEFAULT uuidv7(),
    token               bytea NOT NULL,
    -- Whom the token acts as in Armature, and whether it may only read, as the last check found.
    armature_user_id    uuid,
    armature_user_name  text,
    armature_user_email text,
    read_only           boolean NOT NULL DEFAULT false,
    status              text NOT NULL CHECK (status IN ('ok', 'rejected', 'unreachable')),
    checked_at          timestamptz NOT NULL DEFAULT now(),
    created_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id),
    CONSTRAINT armature_token_id_key UNIQUE (id),
    -- The row goes with the membership, and with the connection it was checked against.
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id) REFERENCES armature_connection (org_id) ON DELETE CASCADE
);

ALTER TABLE armature_connection ENABLE ROW LEVEL SECURITY;
ALTER TABLE armature_connection FORCE  ROW LEVEL SECURITY;
ALTER TABLE armature_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE armature_token FORCE  ROW LEVEL SECURITY;

CREATE POLICY armature_connection_tenant_isolation ON armature_connection
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY armature_connection_admin_bypass ON armature_connection TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY armature_token_tenant_isolation ON armature_token
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY armature_token_admin_bypass ON armature_token TO exciter_admin USING (true) WITH CHECK (true);

-- A new address or Armature organization forgets every stored token, so an administrator can never send the
-- members' tokens to a host of their choosing, and the organization id learned from the old one goes with them.
-- +goose StatementBegin
CREATE FUNCTION armature_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_token WHERE org_id = OLD.org_id;
        NEW.armature_org_id := NULL;
    END IF;
    RETURN NEW;
END;
$$;

-- Where the organization's Armature is, for any member: the base URL links issues and the slug is what a token
-- must belong to. The sealed secret stays behind.
CREATE FUNCTION armature_endpoint() RETURNS TABLE (base_url text, org_slug text, armature_org_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.base_url, c.org_slug, c.armature_org_id
    FROM armature_connection c
    WHERE c.org_id = current_org_id()
      AND EXISTS (SELECT 1 FROM org_member m WHERE m.org_id = current_org_id() AND m.user_id = current_actor_id())
$$;

-- How many members stored a token, for administrators; null for anybody else.
CREATE FUNCTION armature_connected_count() RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN current_actor_administers()
                THEN (SELECT count(*)::integer FROM armature_token WHERE org_id = current_org_id())
           END
$$;
-- +goose StatementEnd

CREATE TRIGGER armature_connection_moved BEFORE UPDATE ON armature_connection
    FOR EACH ROW EXECUTE FUNCTION armature_connection_moved();

-- Administrators keep the connection; each member keeps their own token and nobody reads anybody else's.
CREATE POLICY armature_connection_admins ON armature_connection AS RESTRICTIVE FOR ALL TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());
CREATE POLICY armature_token_own ON armature_token AS RESTRICTIVE FOR ALL TO exciter_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id());

-- The learned organization id is the service's to write as the admin role. A token row's id is always a fresh
-- default, and its token is never rewritten in place: a new token is a new row, so its cache is new too.
GRANT SELECT, INSERT, UPDATE, DELETE ON armature_connection, armature_token TO exciter_admin;
REVOKE ALL ON armature_connection, armature_token FROM exciter_app;
GRANT SELECT, DELETE ON armature_connection TO exciter_app;
GRANT INSERT (org_id, base_url, org_slug, webhook_secret, updated_by) ON armature_connection TO exciter_app;
GRANT UPDATE (base_url, org_slug, webhook_secret, updated_by) ON armature_connection TO exciter_app;
GRANT SELECT, DELETE ON armature_token TO exciter_app;
GRANT INSERT (org_id, user_id, token, armature_user_id, armature_user_name, armature_user_email, read_only, status, checked_at)
    ON armature_token TO exciter_app;
GRANT UPDATE (armature_user_id, armature_user_name, armature_user_email, read_only, status, checked_at) ON armature_token TO exciter_app;
GRANT EXECUTE ON FUNCTION armature_endpoint(), armature_connected_count() TO exciter_app;

-- +goose Down
DROP TABLE IF EXISTS armature_token;
DROP TABLE IF EXISTS armature_connection;
DROP FUNCTION IF EXISTS armature_connected_count();
DROP FUNCTION IF EXISTS armature_endpoint();
DROP FUNCTION IF EXISTS armature_connection_moved();
