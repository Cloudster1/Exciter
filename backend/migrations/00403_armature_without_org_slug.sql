-- +goose Up
-- An Armature installation is one team's tracker too (#87), so its address is what the connection names; the
-- Armature organization learned from the first token stays the check that every token comes from the same one.
-- The function bodies below read the column by name, which no dependency records, so each is replaced first.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION armature_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM armature_token WHERE org_id = OLD.org_id;
        NEW.armature_org_id := NULL;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION armature_connection_forgets_links() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM armature_remote_link WHERE org_id = OLD.org_id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

DROP FUNCTION armature_endpoint();
-- +goose StatementBegin
CREATE FUNCTION armature_endpoint() RETURNS TABLE (base_url text, armature_org_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.base_url, c.armature_org_id
    FROM armature_connection c
    WHERE c.org_id = current_org_id()
      AND EXISTS (SELECT 1 FROM org_member m WHERE m.org_id = current_org_id() AND m.user_id = current_actor_id())
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION armature_endpoint() TO exciter_app;

ALTER TABLE armature_connection DROP COLUMN org_slug;

-- +goose Down
ALTER TABLE armature_connection ADD COLUMN org_slug text;
UPDATE armature_connection c SET org_slug = o.slug FROM org o WHERE o.id = c.org_id;
ALTER TABLE armature_connection ALTER COLUMN org_slug SET NOT NULL,
    ADD CONSTRAINT armature_connection_org_slug_shape CHECK (org_slug ~ '^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$');
GRANT INSERT (org_slug), UPDATE (org_slug) ON armature_connection TO exciter_app;

DROP FUNCTION armature_endpoint();
-- +goose StatementBegin
CREATE FUNCTION armature_endpoint() RETURNS TABLE (base_url text, org_slug text, armature_org_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.base_url, c.org_slug, c.armature_org_id
    FROM armature_connection c
    WHERE c.org_id = current_org_id()
      AND EXISTS (SELECT 1 FROM org_member m WHERE m.org_id = current_org_id() AND m.user_id = current_actor_id())
$$;

CREATE OR REPLACE FUNCTION armature_connection_moved() RETURNS trigger
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

CREATE OR REPLACE FUNCTION armature_connection_forgets_links() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_remote_link WHERE org_id = OLD.org_id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION armature_endpoint() TO exciter_app;
