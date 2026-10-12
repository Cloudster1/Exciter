-- +goose Up
-- An organization may name one of its shared themes as what everybody sees until they choose for themselves. A
-- person's row with no theme is a choice too: the built-in theme, over the organization's default.
ALTER TABLE org ADD COLUMN default_theme_id uuid REFERENCES theme(id) ON DELETE SET NULL;
ALTER TABLE user_theme ALTER COLUMN theme_id DROP NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION org_default_theme_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.default_theme_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM theme WHERE id = NEW.default_theme_id AND org_id = NEW.id AND shared
    ) THEN
        RAISE EXCEPTION 'the organization''s default theme has to be one of its shared themes'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER org_default_theme_check
    BEFORE UPDATE OF default_theme_id ON org
    FOR EACH ROW EXECUTE FUNCTION org_default_theme_guard();

-- Through the app role only the organization's owners and administrators change it, its default theme included;
-- for anybody else an update finds no row.
CREATE POLICY org_admins_change ON org AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());

-- A theme taken private cannot stay the default: it would be shown to people who may no longer see it. SECURITY
-- DEFINER, since the owner who unshares it need not administer the organization.
-- +goose StatementBegin
CREATE FUNCTION theme_unshared_default() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE org SET default_theme_id = NULL WHERE id = NEW.org_id AND default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER theme_unshared_default_check
    AFTER UPDATE OF shared ON theme
    FOR EACH ROW EXECUTE FUNCTION theme_unshared_default();

-- +goose Down
DROP TRIGGER IF EXISTS theme_unshared_default_check ON theme;
DROP FUNCTION IF EXISTS theme_unshared_default();
DROP POLICY IF EXISTS org_admins_change ON org;
DROP TRIGGER IF EXISTS org_default_theme_check ON org;
DROP FUNCTION IF EXISTS org_default_theme_guard();
DELETE FROM user_theme WHERE theme_id IS NULL;
ALTER TABLE user_theme ALTER COLUMN theme_id SET NOT NULL;
ALTER TABLE org DROP COLUMN default_theme_id;
