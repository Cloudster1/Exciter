-- +goose Up
-- Roles that follow the identity provider's groups: which group grants which role, and, per membership, whether
-- its role came from the provider or from somebody here.

-- 'manual' is a role somebody here chose, or the member every membership falls back to; the provider leaves it
-- alone unless a mapped group applies. 'oidc' is a role the provider's groups gave, which the next sign-in may
-- take back.
ALTER TABLE org_member ADD COLUMN role_source text NOT NULL DEFAULT 'manual'
    CHECK (role_source IN ('manual', 'oidc'));

-- The owner is who can always get back in, so no mapping may manage the role; the service never tries, and this
-- makes a later code path that forgets fail rather than lock the organization out.
ALTER TABLE org_member ADD CONSTRAINT org_member_owner_is_manual
    CHECK (org_role <> 'owner' OR role_source = 'manual');

-- +goose StatementBegin
CREATE FUNCTION org_member_owner_not_by_provider() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.org_role = 'owner' AND NEW.org_role <> 'owner' AND NEW.role_source = 'oidc' THEN
        RAISE EXCEPTION 'the identity provider cannot change the role of an organization''s owner'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER org_member_owner_not_by_provider BEFORE UPDATE OF org_role, role_source ON org_member
    FOR EACH ROW EXECUTE FUNCTION org_member_owner_not_by_provider();

-- Belongs to the provider rather than the organization: a provider removed takes its mapping with it, since the
-- group names meant something only there. Several groups may grant the same role; each group grants one.
CREATE TABLE oidc_group_role (
    org_id     uuid NOT NULL REFERENCES oidc_provider(org_id) ON DELETE CASCADE,
    id         uuid NOT NULL DEFAULT uuidv7(),
    -- A value of the provider's groups claim, matched exactly as sent.
    group_ref  text NOT NULL,
    org_role   text NOT NULL CHECK (org_role IN ('admin', 'member')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    CONSTRAINT oidc_group_role_ref_not_blank CHECK (btrim(group_ref) <> '')
);

CREATE UNIQUE INDEX oidc_group_role_ref_idx ON oidc_group_role (org_id, group_ref);

CREATE TRIGGER oidc_group_role_set_updated_at BEFORE UPDATE ON oidc_group_role
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE oidc_group_role ENABLE ROW LEVEL SECURITY;
ALTER TABLE oidc_group_role FORCE  ROW LEVEL SECURITY;

CREATE POLICY oidc_group_role_tenant_isolation ON oidc_group_role
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY oidc_group_role_admin_bypass ON oidc_group_role TO exciter_admin USING (true) WITH CHECK (true);

-- A mapping hands out roles, so through the app role only the organization's administrators read or change it;
-- a sign-in follows it through the admin role.
CREATE POLICY oidc_group_role_admins_only ON oidc_group_role AS RESTRICTIVE FOR ALL TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());

GRANT SELECT, INSERT, UPDATE, DELETE ON oidc_group_role TO exciter_app, exciter_admin;

-- +goose Down
DROP TABLE IF EXISTS oidc_group_role;
DROP TRIGGER IF EXISTS org_member_owner_not_by_provider ON org_member;
DROP FUNCTION IF EXISTS org_member_owner_not_by_provider();
ALTER TABLE org_member DROP CONSTRAINT IF EXISTS org_member_owner_is_manual;
ALTER TABLE org_member DROP COLUMN IF EXISTS role_source;
