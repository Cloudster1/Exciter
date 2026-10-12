-- +goose Up
-- Somebody the identity provider vouches for, who has not been let in. The row is what an administrator sees under
-- single sign-on, so letting them in is a click there rather than an invitation typed out and mailed.
CREATE TABLE org_join_request (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);

ALTER TABLE org_join_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_join_request FORCE  ROW LEVEL SECURITY;

CREATE POLICY org_join_request_tenant_isolation ON org_join_request
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY org_join_request_admin_bypass ON org_join_request TO exciter_admin USING (true) WITH CHECK (true);

-- Answering a request is letting somebody in, which through the app role only the organization's administrators
-- do; the sign-in notes a request through the admin role, before anybody is signed in.
CREATE POLICY org_join_request_admins_only ON org_join_request AS RESTRICTIVE FOR ALL TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());

GRANT SELECT, INSERT, UPDATE, DELETE ON org_join_request TO exciter_app, exciter_admin;

-- +goose Down
DROP TABLE IF EXISTS org_join_request;
