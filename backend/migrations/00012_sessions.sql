-- +goose Up
-- Browser sessions, local passwords and the guest role.

-- NULL for somebody who only ever signs in through a provider.
ALTER TABLE app_user ADD COLUMN password_hash text;

-- No locale is no choice: the interface follows the browser.
ALTER TABLE app_user ALTER COLUMN locale DROP NOT NULL, ALTER COLUMN locale DROP DEFAULT;
ALTER TABLE app_user ADD CONSTRAINT app_user_locale_known CHECK (locale IN ('en', 'de'));

-- A guest is somebody from outside, let into the projects #12 grants them and nothing else.
ALTER TABLE org_member DROP CONSTRAINT org_member_org_role_check;
ALTER TABLE org_member ADD CONSTRAINT org_member_org_role_check CHECK (org_role IN ('owner', 'admin', 'member', 'guest'));

-- The owner is how the organization is always entered again, so the database keeps one, whatever asks.
-- +goose StatementBegin
CREATE FUNCTION org_member_owner_stays() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.org_role = 'owner' AND (TG_OP = 'DELETE' OR NEW.org_role <> 'owner')
       AND EXISTS (SELECT 1 FROM org WHERE id = OLD.org_id)
       AND NOT EXISTS (SELECT 1 FROM org_member WHERE org_id = OLD.org_id AND user_id <> OLD.user_id AND org_role = 'owner') THEN
        RAISE EXCEPTION 'an organization keeps its last owner' USING ERRCODE = 'check_violation';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER org_member_owner_stays BEFORE UPDATE OF org_role OR DELETE ON org_member
    FOR EACH ROW EXECUTE FUNCTION org_member_owner_stays();

-- Whether the transaction acts for an owner or administrator of its organization. SECURITY DEFINER, so the
-- policies on org_member below can ask it without asking themselves again.
-- +goose StatementBegin
CREATE FUNCTION current_actor_administers() RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM org_member
        WHERE org_id = current_org_id() AND user_id = current_actor_id() AND org_role IN ('owner', 'admin'))
$$;
-- +goose StatementEnd

-- Through the app role only the organization's administrators change who belongs to it, and nobody is made
-- an owner that way; the bootstrap, which runs as the admin role, makes owners.
CREATE POLICY org_member_admins_add ON org_member AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (current_actor_administers() AND org_role <> 'owner');
CREATE POLICY org_member_admins_change ON org_member AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers() AND org_role <> 'owner');
CREATE POLICY org_member_admins_remove ON org_member AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (current_actor_administers());

-- A session spans organizations because a person switches without signing in again; current_org_id is
-- where it acts now. Only the SHA-256 of the token is stored, so a copy of this table opens no session.
CREATE TABLE user_session (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id        uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    token_hash     bytea NOT NULL UNIQUE,
    current_org_id uuid REFERENCES org(id) ON DELETE SET NULL,
    -- How the session was proven, and for which organization, which decides where it may go.
    proof          text NOT NULL CHECK (proof IN ('password', 'oidc')),
    proof_org_id   uuid REFERENCES org(id) ON DELETE SET NULL,
    user_agent     text,
    ip             inet,
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_seen_at   timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL,
    CONSTRAINT user_session_token_is_sha256 CHECK (octet_length(token_hash) = 32)
);

CREATE INDEX user_session_user_idx ON user_session (user_id);
CREATE INDEX user_session_expiry_idx ON user_session (expires_at);

-- Where a session may go. A password vouches for the person everywhere; a provider's sign-in only for the
-- organization it was made in, until #7 adds the organizations that trust the same provider.
-- +goose StatementBegin
CREATE FUNCTION session_reaches(proof text, proof_org uuid, person uuid, target uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT proof = 'password' OR target = proof_org
$$;
-- +goose StatementEnd

-- The service asks session_reaches before switching; the trigger asks again, so a later code path that
-- forgets to cannot move a session where it may not go.
-- +goose StatementBegin
CREATE FUNCTION session_stays_home() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.proof IS DISTINCT FROM OLD.proof THEN
        RAISE EXCEPTION 'a session keeps the proof it was opened with' USING ERRCODE = 'check_violation';
    END IF;
    -- The organization going, which nulls the column, is the one way it changes.
    IF NEW.proof_org_id IS NOT NULL AND NEW.proof_org_id IS DISTINCT FROM OLD.proof_org_id THEN
        RAISE EXCEPTION 'a session keeps the organization it was proven for' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.current_org_id IS NOT NULL AND NEW.current_org_id IS DISTINCT FROM OLD.current_org_id
       AND NOT session_reaches(OLD.proof, OLD.proof_org_id, OLD.user_id, NEW.current_org_id) THEN
        RAISE EXCEPTION 'a session opened by % does not reach that organization', OLD.proof
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER session_stays_home BEFORE UPDATE OF current_org_id, proof, proof_org_id ON user_session
    FOR EACH ROW EXECUTE FUNCTION session_stays_home();

-- Sessions are looked up by token before any tenant is known, so only the admin role reaches them; the
-- application role sees none, in any tenant.
ALTER TABLE user_session ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_session FORCE  ROW LEVEL SECURITY;
CREATE POLICY user_session_admin_bypass ON user_session TO exciter_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON user_session TO exciter_app, exciter_admin;

-- +goose Down
DROP TABLE IF EXISTS user_session CASCADE;
DROP FUNCTION IF EXISTS session_stays_home();
DROP FUNCTION IF EXISTS session_reaches(text, uuid, uuid, uuid);
DROP POLICY IF EXISTS org_member_admins_remove ON org_member;
DROP POLICY IF EXISTS org_member_admins_change ON org_member;
DROP POLICY IF EXISTS org_member_admins_add ON org_member;
DROP FUNCTION IF EXISTS current_actor_administers();
DROP TRIGGER IF EXISTS org_member_owner_stays ON org_member;
DROP FUNCTION IF EXISTS org_member_owner_stays();
ALTER TABLE org_member DROP CONSTRAINT org_member_org_role_check;
ALTER TABLE org_member ADD CONSTRAINT org_member_org_role_check CHECK (org_role IN ('owner', 'admin', 'member'));
ALTER TABLE app_user DROP CONSTRAINT IF EXISTS app_user_locale_known;
UPDATE app_user SET locale = 'en' WHERE locale IS NULL;
ALTER TABLE app_user ALTER COLUMN locale SET DEFAULT 'en', ALTER COLUMN locale SET NOT NULL;
ALTER TABLE app_user DROP COLUMN IF EXISTS password_hash;
