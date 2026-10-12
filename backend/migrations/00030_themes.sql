-- +goose Up
-- A theme is a saved redefinition of the interface's tokens, with the files it draws from. It is its maker's,
-- shared with the organization when they say so, and chosen per person; deleting it returns its people to the
-- built-in theme by the cascade. Adapted from Stator's, which is Armature's, whose theme files import here.
CREATE TABLE theme (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    owner_id   uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    name       text NOT NULL,
    shared     boolean NOT NULL DEFAULT false,
    spec       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT theme_name_not_blank CHECK (btrim(name) <> ''),
    -- A foreign key is checked past row level security, so the rows that point at a theme name its organization
    -- too and cannot cross tenants.
    CONSTRAINT theme_org_id_key UNIQUE (org_id, id)
);

-- Unique per owner within an organization, so saving an example in a second organization is not refused for a
-- theme the person cannot see from there.
CREATE UNIQUE INDEX theme_owner_name_idx ON theme (org_id, owner_id, lower(name));
CREATE INDEX theme_org_idx ON theme (org_id, shared);

CREATE TRIGGER theme_set_updated_at BEFORE UPDATE ON theme
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A theme stays its maker's: an administrator may tidy a shared one, never take it over.
-- +goose StatementBegin
CREATE FUNCTION theme_owner_stays() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.owner_id IS DISTINCT FROM OLD.owner_id THEN
        RAISE EXCEPTION 'a theme keeps the person who made it' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER theme_owner_stays BEFORE UPDATE OF owner_id ON theme
    FOR EACH ROW EXECUTE FUNCTION theme_owner_stays();

-- The files a theme draws from. The bytes live in the object store under org/<org>/theme/<theme>/<asset>; the
-- row is what says the file exists.
CREATE TABLE theme_asset (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    theme_id     uuid NOT NULL,
    name         text NOT NULL,
    content_type text NOT NULL,
    size         bigint NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, theme_id) REFERENCES theme (org_id, id) ON DELETE CASCADE
);

CREATE INDEX theme_asset_theme_idx ON theme_asset (theme_id);

-- Who chose what, one row per person per organization. A row with no theme is a choice too: the built-in theme
-- over the organization's default, which 00031 brings.
CREATE TABLE user_theme (
    org_id   uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    theme_id uuid NOT NULL,
    PRIMARY KEY (org_id, user_id),
    FOREIGN KEY (org_id, theme_id) REFERENCES theme (org_id, id) ON DELETE CASCADE
);

CREATE INDEX user_theme_theme_idx ON user_theme (theme_id);

ALTER TABLE theme       ENABLE ROW LEVEL SECURITY;
ALTER TABLE theme       FORCE  ROW LEVEL SECURITY;
ALTER TABLE theme_asset ENABLE ROW LEVEL SECURITY;
ALTER TABLE theme_asset FORCE  ROW LEVEL SECURITY;
ALTER TABLE user_theme  ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_theme  FORCE  ROW LEVEL SECURITY;

CREATE POLICY theme_tenant_isolation ON theme
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY theme_asset_tenant_isolation ON theme_asset
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY user_theme_tenant_isolation ON user_theme
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());

CREATE POLICY theme_admin_bypass ON theme TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY theme_asset_admin_bypass ON theme_asset TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY user_theme_admin_bypass ON user_theme TO exciter_admin USING (true) WITH CHECK (true);

-- Through the app role a person sees their own themes and the shared ones, makes themes only in their own name,
-- and changes or deletes their own, or, as an administrator, a shared one.
CREATE POLICY theme_visible ON theme AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (owner_id = current_actor_id() OR shared);
CREATE POLICY theme_made_as_oneself ON theme AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (owner_id = current_actor_id());
CREATE POLICY theme_changed_by_owner ON theme AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (owner_id = current_actor_id() OR (shared AND current_actor_administers()))
    WITH CHECK (owner_id = current_actor_id() OR current_actor_administers());
CREATE POLICY theme_deleted_by_owner ON theme AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (owner_id = current_actor_id() OR (shared AND current_actor_administers()));

-- A file follows its theme: seen with it, and added or removed by whoever may change it. The subqueries on theme
-- pass through its own policies above.
CREATE POLICY theme_asset_visible ON theme_asset AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id));
CREATE POLICY theme_asset_added_by_owner ON theme_asset AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id
                        AND (t.owner_id = current_actor_id() OR (t.shared AND current_actor_administers()))));
CREATE POLICY theme_asset_changed_by_owner ON theme_asset AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id
                   AND (t.owner_id = current_actor_id() OR (t.shared AND current_actor_administers()))));
CREATE POLICY theme_asset_removed_by_owner ON theme_asset AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id
                   AND (t.owner_id = current_actor_id() OR (t.shared AND current_actor_administers()))));

-- A choice is the chooser's own, and of a theme they may see: the foreign key alone would take anybody's.
CREATE POLICY user_theme_own_choice ON user_theme AS RESTRICTIVE FOR ALL TO exciter_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id()
                AND (theme_id IS NULL OR EXISTS (SELECT 1 FROM theme t WHERE t.org_id = user_theme.org_id AND t.id = user_theme.theme_id)));

-- How many people chose a theme the caller may see. SECURITY DEFINER, since each person sees only their own
-- choice; it answers 0 for a theme out of the caller's sight.
-- +goose StatementBegin
CREATE FUNCTION theme_in_use(theme uuid) RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT count(*)::integer FROM user_theme ut
    WHERE ut.org_id = current_org_id() AND ut.theme_id = theme_in_use.theme
      AND EXISTS (SELECT 1 FROM theme t WHERE t.org_id = ut.org_id AND t.id = ut.theme_id
                  AND (t.owner_id = current_actor_id() OR t.shared))
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT, UPDATE, DELETE ON theme, theme_asset, user_theme TO exciter_app, exciter_admin;

-- +goose Down
DROP FUNCTION IF EXISTS theme_in_use(uuid);
DROP TABLE IF EXISTS user_theme;
DROP TABLE IF EXISTS theme_asset;
DROP TABLE IF EXISTS theme;
DROP FUNCTION IF EXISTS theme_owner_stays();
