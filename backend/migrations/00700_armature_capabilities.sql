-- +goose Up
-- What the connected Armature lists among its capabilities on GET /meta (#100), so a feature it lacks says so
-- instead of failing later. Null until it is asked, which counts as offering everything: an Exciter upgraded before
-- anybody saves the connection again switches nothing off.
ALTER TABLE armature_connection ADD COLUMN capabilities text[];

-- +goose StatementBegin
-- A new address forgets every stored token and what the old one offered, until the new one is asked.
CREATE OR REPLACE FUNCTION armature_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM armature_token;
        NEW.armature_org_id := NULL;
        NEW.capabilities := NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP FUNCTION armature_endpoint();
-- +goose StatementBegin
-- Where the installation's Armature is and what it offers, for any member; the sealed secret stays behind.
CREATE FUNCTION armature_endpoint() RETURNS TABLE (base_url text, armature_org_id uuid, capabilities text[])
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.base_url, c.armature_org_id, c.capabilities
    FROM armature_connection c
    WHERE EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.role IS NOT NULL)
$$;
-- +goose StatementEnd
-- The service writes what Armature offers as the admin role, so nobody claims a capability with raw SQL: the
-- column is outside exciter_app's INSERT and UPDATE grants.
REVOKE ALL ON FUNCTION armature_endpoint() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION armature_endpoint() TO exciter_app, exciter_admin;

-- +goose Down
DROP FUNCTION armature_endpoint();
-- +goose StatementBegin
CREATE FUNCTION armature_endpoint() RETURNS TABLE (base_url text, armature_org_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.base_url, c.armature_org_id
    FROM armature_connection c
    WHERE EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.role IS NOT NULL)
$$;

CREATE OR REPLACE FUNCTION armature_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM armature_token;
        NEW.armature_org_id := NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION armature_endpoint() TO exciter_app;
ALTER TABLE armature_connection DROP COLUMN capabilities;
