-- +goose Up
-- Who is in a group decides what its grants reach, so through the app role only the installation's administrators
-- write groups or their members, as the service allows (#335). Reading stays open: the pickers, grants naming a
-- group and the reviews that resolve one read them for whoever acts. Sign-in and a partner's token exchange keep
-- provider groups in step through the admin role, which these do not bind.
CREATE POLICY groups_admins_add ON groups AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (current_actor_administers());
CREATE POLICY groups_admins_change ON groups AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());
CREATE POLICY groups_admins_remove ON groups AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (current_actor_administers());

CREATE POLICY group_member_admins_add ON group_member AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (current_actor_administers());
CREATE POLICY group_member_admins_change ON group_member AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());
CREATE POLICY group_member_admins_remove ON group_member AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (current_actor_administers());

-- +goose Down
DROP POLICY IF EXISTS group_member_admins_remove ON group_member;
DROP POLICY IF EXISTS group_member_admins_change ON group_member;
DROP POLICY IF EXISTS group_member_admins_add ON group_member;
DROP POLICY IF EXISTS groups_admins_remove ON groups;
DROP POLICY IF EXISTS groups_admins_change ON groups;
DROP POLICY IF EXISTS groups_admins_add ON groups;
