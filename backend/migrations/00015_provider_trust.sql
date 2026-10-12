-- +goose Up
-- Who may configure an organization's identity provider, and how far a sign-in through one reaches.

-- Through the app role only the organization's administrators read or change its provider, as they alone change
-- its members; a sign-in reads it through the admin role before anybody is signed in.
CREATE POLICY oidc_provider_admins_only ON oidc_provider AS RESTRICTIVE FOR ALL TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());

-- Where a session may go. A password vouches for the person everywhere. A provider's sign-in vouches for them
-- wherever that same provider is trusted, and in an organization they own; otherwise an organization's own
-- provider could vouch for somebody in another organization that never trusted it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION session_reaches(proof text, proof_org uuid, person uuid, target uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT proof = 'password'
        OR target = proof_org
        OR (proof = 'oidc' AND (
            EXISTS (SELECT 1 FROM org_member
                    WHERE org_id = target AND user_id = person AND org_role = 'owner')
            OR EXISTS (SELECT 1 FROM oidc_provider home
                       JOIN oidc_provider away ON away.issuer = home.issuer
                       WHERE home.org_id = proof_org AND away.org_id = target
                         AND home.enabled AND away.enabled)))
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION session_reaches(proof text, proof_org uuid, person uuid, target uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT proof = 'password' OR target = proof_org
$$;
-- +goose StatementEnd
DROP POLICY IF EXISTS oidc_provider_admins_only ON oidc_provider;
