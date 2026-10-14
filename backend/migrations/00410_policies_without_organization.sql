-- +goose Up
-- An installation holds one organization (#82), so the tenant policies keep nothing apart (#106). Each becomes one
-- open policy for every role, which leaves the permission policies, all restrictive and all for exciter_app, as the
-- only walls; FORCE stays. The admin role's bypass is then the open policy, except on the tables only it may read.

-- A policy these migrations did not make would vanish with the tenant layer unnoticed; the upgrade names it instead.
-- +goose StatementBegin
DO $$
DECLARE
    unknown text;
BEGIN
    SELECT string_agg(format('%s on %s', policyname, tablename), ', ' ORDER BY tablename, policyname) INTO unknown
    FROM pg_policies
    WHERE schemaname = 'public' AND permissive = 'PERMISSIVE'
      AND policyname <> tablename || '_tenant_isolation' AND policyname <> tablename || '_admin_bypass';
    IF unknown IS NOT NULL THEN
        RAISE EXCEPTION 'This database has policies Exciter did not make: %. Drop them or fold them into the permission policies, then upgrade again.', unknown;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOR t IN SELECT tablename FROM pg_policies WHERE schemaname = 'public' AND policyname = tablename || '_tenant_isolation' LOOP
        EXECUTE format('DROP POLICY %I ON %I', t || '_tenant_isolation', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', t || '_admin_bypass', t);
        IF t = 'app_user' THEN
            -- The app role still sees only the people who belong to the installation, not those waiting to be let
            -- in; the admin role, which signs in and admits them, sees every account.
            EXECUTE 'CREATE POLICY app_user_installation ON app_user TO exciter_app
                     USING (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id))
                     WITH CHECK (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id))';
            EXECUTE 'CREATE POLICY app_user_admin_bypass ON app_user TO exciter_admin USING (true) WITH CHECK (true)';
        ELSE
            EXECUTE format('CREATE POLICY %I ON %I USING (true) WITH CHECK (true)', t || '_installation', t);
        END IF;
    END LOOP;
END $$;
-- +goose StatementEnd

-- What the tenant policies kept from the app role by accident becomes a privilege it does not hold: a sign-in in
-- flight, with its nonce and verifier, is the admin role's alone, and nobody makes or removes the installation.
REVOKE ALL ON oidc_login FROM exciter_app;
REVOKE INSERT, DELETE ON org FROM exciter_app;

-- The permission policies that also asked for the organization ask what they meant without it.
ALTER POLICY audit_log_admins_read ON audit_log
    USING ((SELECT EXISTS (SELECT 1 FROM org_member m
                           WHERE m.user_id = current_actor_id() AND m.org_role IN ('owner', 'admin'))));
ALTER POLICY theme_asset_visible ON theme_asset
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.id = theme_asset.theme_id));
ALTER POLICY theme_asset_added_by_owner ON theme_asset
    WITH CHECK (EXISTS (SELECT 1 FROM theme t WHERE t.id = theme_asset.theme_id
                        AND (t.owner_id = current_actor_id() OR (t.shared AND current_actor_administers()))));
ALTER POLICY theme_asset_changed_by_owner ON theme_asset
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.id = theme_asset.theme_id
                   AND (t.owner_id = current_actor_id() OR (t.shared AND current_actor_administers()))));
ALTER POLICY theme_asset_removed_by_owner ON theme_asset
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.id = theme_asset.theme_id
                   AND (t.owner_id = current_actor_id() OR (t.shared AND current_actor_administers()))));
ALTER POLICY user_theme_own_choice ON user_theme
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id()
                AND (theme_id IS NULL OR EXISTS (SELECT 1 FROM theme t WHERE t.id = user_theme.theme_id)));
ALTER POLICY api_token_project_seen_with_token ON api_token_project
    USING (perm_token_whole(current_actor_id())
           AND EXISTS (SELECT 1 FROM api_token t WHERE t.id = api_token_project.token_id));

-- +goose Down
GRANT SELECT, INSERT, UPDATE, DELETE ON oidc_login TO exciter_app;
GRANT INSERT, DELETE ON org TO exciter_app;
ALTER POLICY audit_log_admins_read ON audit_log
    USING ((SELECT EXISTS (SELECT 1 FROM org_member m
                           WHERE m.org_id = current_org_id() AND m.user_id = current_actor_id()
                             AND m.org_role IN ('owner', 'admin'))));
ALTER POLICY theme_asset_visible ON theme_asset
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id));
ALTER POLICY theme_asset_added_by_owner ON theme_asset
    WITH CHECK (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id
                        AND (t.owner_id = current_actor_id() OR (t.shared AND current_actor_administers()))));
ALTER POLICY theme_asset_changed_by_owner ON theme_asset
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id
                   AND (t.owner_id = current_actor_id() OR (t.shared AND current_actor_administers()))));
ALTER POLICY theme_asset_removed_by_owner ON theme_asset
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id
                   AND (t.owner_id = current_actor_id() OR (t.shared AND current_actor_administers()))));
ALTER POLICY user_theme_own_choice ON user_theme
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id()
                AND (theme_id IS NULL OR EXISTS (SELECT 1 FROM theme t WHERE t.org_id = user_theme.org_id AND t.id = user_theme.theme_id)));
ALTER POLICY api_token_project_seen_with_token ON api_token_project
    USING (perm_token_whole(current_actor_id())
           AND EXISTS (SELECT 1 FROM api_token t WHERE t.org_id = api_token_project.org_id AND t.id = api_token_project.token_id));

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOR t IN SELECT tablename FROM pg_policies WHERE schemaname = 'public' AND policyname = tablename || '_installation' LOOP
        EXECUTE format('DROP POLICY %I ON %I', t || '_installation', t);
        IF t = 'app_user' THEN
            EXECUTE 'CREATE POLICY app_user_tenant_isolation ON app_user
                     USING (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id AND m.org_id = current_org_id()))
                     WITH CHECK (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id AND m.org_id = current_org_id()))';
        ELSIF t = 'org' THEN
            EXECUTE 'CREATE POLICY org_tenant_isolation ON org USING (id = current_org_id()) WITH CHECK (id = current_org_id())';
            EXECUTE 'CREATE POLICY org_admin_bypass ON org TO exciter_admin USING (true) WITH CHECK (true)';
        ELSE
            EXECUTE format('CREATE POLICY %I ON %I USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id())',
                           t || '_tenant_isolation', t);
            EXECUTE format('CREATE POLICY %I ON %I TO exciter_admin USING (true) WITH CHECK (true)', t || '_admin_bypass', t);
        END IF;
    END LOOP;
END $$;
-- +goose StatementEnd
