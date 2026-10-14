-- +goose Up
-- Following the Armature theme (#30), Stator's 00173. A person who follows keeps a row in user_theme naming no
-- theme, marked follow_armature; the theme Armature shows them is kept as their mirror, a theme of theirs marked
-- with the id and updatedAt of Armature's, downloaded again only when either changes.
ALTER TABLE theme
    ADD COLUMN armature_theme_id   uuid,
    ADD COLUMN armature_updated_at timestamptz,
    ADD CONSTRAINT theme_mirror_complete CHECK ((armature_theme_id IS NULL) = (armature_updated_at IS NULL)),
    -- A mirror is the person's own copy of what Armature shows them, so it is never shown to anybody else.
    ADD CONSTRAINT theme_mirror_private CHECK (armature_theme_id IS NULL OR NOT shared);

-- A mirror takes Armature's name, which may be the name of a theme the person made themselves.
DROP INDEX theme_owner_name_idx;
CREATE UNIQUE INDEX theme_owner_name_idx ON theme (owner_id, lower(name)) WHERE armature_theme_id IS NULL;
CREATE INDEX theme_mirror_idx ON theme (owner_id) WHERE armature_theme_id IS NOT NULL;

-- Choosing a theme ends following: a row cannot name a theme and follow.
ALTER TABLE user_theme
    ADD COLUMN follow_armature boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT user_theme_follow_alone CHECK (NOT follow_armature OR theme_id IS NULL);

-- A mirror is applied by following, never chosen like a theme, so it goes when following ends rather than
-- lingering as somebody's choice.
-- +goose StatementBegin
CREATE FUNCTION user_theme_not_mirror() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.theme_id IS NOT NULL AND EXISTS (
        SELECT 1 FROM theme WHERE id = NEW.theme_id AND armature_theme_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'a copy of an Armature theme is followed, not chosen'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER user_theme_not_mirror BEFORE INSERT OR UPDATE OF theme_id ON user_theme
    FOR EACH ROW EXECUTE FUNCTION user_theme_not_mirror();

-- +goose Down
DROP TRIGGER user_theme_not_mirror ON user_theme;
DROP FUNCTION user_theme_not_mirror();
DELETE FROM user_theme WHERE follow_armature;
ALTER TABLE user_theme DROP CONSTRAINT user_theme_follow_alone, DROP COLUMN follow_armature;
DELETE FROM theme WHERE armature_theme_id IS NOT NULL;
DROP INDEX theme_mirror_idx;
DROP INDEX theme_owner_name_idx;
CREATE UNIQUE INDEX theme_owner_name_idx ON theme (owner_id, lower(name));
ALTER TABLE theme
    DROP CONSTRAINT theme_mirror_private,
    DROP CONSTRAINT theme_mirror_complete,
    DROP COLUMN armature_updated_at,
    DROP COLUMN armature_theme_id;
