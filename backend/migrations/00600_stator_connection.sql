-- +goose Up
-- The Stator instance the installation connects (#40), and each member's token for it, as the Armature connection
-- keeps them (00300, as it stands since 00415). One row: where people open Stator, whether it takes remote links on
-- pages (#42), and the secret Stator signs webhooks with, sealed with EXCITER_SECRET_KEY.
CREATE TABLE stator_connection (
    -- An origin: a scheme, a host and maybe a port, nothing after it. The service says why in words; this keeps
    -- raw SQL to the same shape.
    base_url       text NOT NULL,
    -- Read from Stator's /meta by the service as the admin role, so nobody claims it with SQL.
    remote_links   boolean NOT NULL DEFAULT false,
    webhook_secret bytea,
    updated_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT stator_connection_base_url_origin CHECK (base_url ~ '^https?://[^/?#@[:space:]]+$')
);
CREATE UNIQUE INDEX stator_connection_one_row_idx ON stator_connection ((true));

CREATE TRIGGER stator_connection_set_updated_at BEFORE UPDATE ON stator_connection
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A member's Stator personal access token, sealed and bound to the installation and the person. Every call to
-- Stator is made with the viewer's own, so Exciter shows a page only to somebody Stator shows it to.
CREATE TABLE stator_token (
    user_id           uuid PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,
    -- New with every token stored, so a cache entry keyed by it is never read for another token.
    id                uuid NOT NULL DEFAULT uuidv7(),
    token             bytea NOT NULL,
    -- Whom the token acts as in Stator, as the last check found.
    stator_user_id    uuid,
    stator_user_name  text,
    stator_user_email text,
    status            text NOT NULL CHECK (status IN ('ok', 'rejected', 'unreachable')),
    checked_at        timestamptz NOT NULL DEFAULT now(),
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT stator_token_id_key UNIQUE (id)
);

CREATE TRIGGER stator_token_held_by_a_member BEFORE INSERT OR UPDATE OF user_id ON stator_token
    FOR EACH ROW EXECUTE FUNCTION held_by_a_member('user_id');
CREATE TRIGGER stator_token_needs_a_connection BEFORE INSERT ON stator_token
    FOR EACH ROW EXECUTE FUNCTION needs_its_one_row('stator_connection');

-- +goose StatementBegin
-- A new address forgets every stored token, so an administrator can never send the members' tokens to a host of
-- their choosing, and what the old one offered goes with them until the new one is asked.
CREATE FUNCTION stator_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM stator_token;
        NEW.remote_links := false;
    END IF;
    RETURN NEW;
END;
$$;

-- The tokens go with the connection; they hold no key to it, since it has none to hold.
CREATE FUNCTION stator_connection_forgets() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM stator_token;
    RETURN NULL;
END;
$$;

-- Where the installation's Stator is, for any member: the base URL links pages, and whether it takes remote links
-- says whether the worker puts any there. The sealed secret stays behind.
CREATE FUNCTION stator_endpoint() RETURNS TABLE (base_url text, remote_links boolean)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.base_url, c.remote_links
    FROM stator_connection c
    WHERE EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.role IS NOT NULL)
$$;

-- How many members stored a token, for administrators; null for anybody else.
CREATE FUNCTION stator_connected_count() RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN current_actor_administers()
                THEN (SELECT count(*)::integer FROM stator_token)
           END
$$;

-- A role taken away takes what only a member holds, the Stator token now too.
CREATE OR REPLACE FUNCTION app_user_left() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM api_token WHERE user_id = NEW.id;
    DELETE FROM armature_token WHERE user_id = NEW.id;
    DELETE FROM stator_token WHERE user_id = NEW.id;
    DELETE FROM project_grant WHERE user_id = NEW.id;
    UPDATE requirement SET owner_id = NULL WHERE owner_id = NEW.id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER stator_connection_moved BEFORE UPDATE ON stator_connection
    FOR EACH ROW EXECUTE FUNCTION stator_connection_moved();
CREATE TRIGGER stator_connection_forgets AFTER DELETE ON stator_connection
    FOR EACH ROW EXECUTE FUNCTION stator_connection_forgets();

ALTER TABLE stator_connection ENABLE ROW LEVEL SECURITY;
ALTER TABLE stator_connection FORCE  ROW LEVEL SECURITY;
ALTER TABLE stator_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE stator_token FORCE  ROW LEVEL SECURITY;

-- One open policy per table, as every table has since #106, and the permission policies beside it.
CREATE POLICY stator_connection_installation ON stator_connection USING (true) WITH CHECK (true);
CREATE POLICY stator_token_installation ON stator_token USING (true) WITH CHECK (true);

-- Administrators keep the connection; each member keeps their own token and nobody reads anybody else's.
CREATE POLICY stator_connection_admins ON stator_connection AS RESTRICTIVE FOR ALL TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());
CREATE POLICY stator_token_own ON stator_token AS RESTRICTIVE FOR ALL TO exciter_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id());

-- What Stator offers is the service's to write as the admin role. A token row's id is always a fresh default, and
-- its token is never rewritten in place: a new token is a new row, so its cache is new too.
GRANT SELECT, INSERT, UPDATE, DELETE ON stator_connection, stator_token TO exciter_admin;
REVOKE ALL ON stator_connection, stator_token FROM exciter_app;
GRANT SELECT, DELETE ON stator_connection TO exciter_app;
GRANT INSERT (base_url, webhook_secret, updated_by), UPDATE (base_url, webhook_secret, updated_by)
    ON stator_connection TO exciter_app;
GRANT SELECT, DELETE ON stator_token TO exciter_app;
GRANT INSERT (user_id, token, stator_user_id, stator_user_name, stator_user_email, status, checked_at)
    ON stator_token TO exciter_app;
GRANT UPDATE (stator_user_id, stator_user_name, stator_user_email, status, checked_at) ON stator_token TO exciter_app;
REVOKE ALL ON FUNCTION stator_endpoint(), stator_connected_count() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION stator_endpoint(), stator_connected_count() TO exciter_app, exciter_admin;

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_user_left() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM api_token WHERE user_id = NEW.id;
    DELETE FROM armature_token WHERE user_id = NEW.id;
    DELETE FROM project_grant WHERE user_id = NEW.id;
    UPDATE requirement SET owner_id = NULL WHERE owner_id = NEW.id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
DROP TABLE stator_token;
DROP TABLE stator_connection;
DROP FUNCTION stator_connected_count();
DROP FUNCTION stator_endpoint();
DROP FUNCTION stator_connection_forgets();
DROP FUNCTION stator_connection_moved();
