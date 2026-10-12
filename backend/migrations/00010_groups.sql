-- +goose Up
-- Groups: a name for a set of people in one organization. Members come either
-- from the identity provider, which then owns them, or from somebody adding
-- them by hand, which the provider leaves alone.

CREATE TABLE groups (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    source       text NOT NULL DEFAULT 'local' CHECK (source IN ('local', 'oidc')),
    -- The value the provider sends in its groups claim. A claim is matched on
    -- this rather than on the name, so renaming a group keeps the mapping.
    external_ref text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT groups_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT groups_external_ref_belongs_to_oidc CHECK (source = 'oidc' OR external_ref IS NULL)
);

CREATE UNIQUE INDEX groups_org_name_idx ON groups (org_id, lower(name));
CREATE UNIQUE INDEX groups_org_ref_idx ON groups (org_id, external_ref) WHERE external_ref IS NOT NULL;

CREATE TRIGGER groups_set_updated_at BEFORE UPDATE ON groups
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE group_member (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    group_id   uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX group_member_user_idx ON group_member (org_id, user_id);

-- The policy keeps a row inside its tenant; this keeps the row's group and
-- person inside the same tenant as the row, which the policy cannot see.
-- +goose StatementBegin
CREATE FUNCTION group_member_scope_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (SELECT org_id FROM groups WHERE id = NEW.group_id) IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a group member cannot cross organizations' USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id) THEN
        RAISE EXCEPTION 'only a member of the organization can join one of its groups' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER group_member_scope_check BEFORE INSERT OR UPDATE ON group_member
    FOR EACH ROW EXECUTE FUNCTION group_member_scope_guard();

ALTER TABLE groups       ENABLE ROW LEVEL SECURITY;
ALTER TABLE groups       FORCE  ROW LEVEL SECURITY;
ALTER TABLE group_member ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_member FORCE  ROW LEVEL SECURITY;

CREATE POLICY groups_tenant_isolation ON groups
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY group_member_tenant_isolation ON group_member
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY groups_admin_bypass ON groups TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY group_member_admin_bypass ON group_member TO exciter_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON groups, group_member TO exciter_app, exciter_admin;

-- +goose Down
DROP TABLE IF EXISTS group_member, groups CASCADE;
DROP FUNCTION IF EXISTS group_member_scope_guard();
