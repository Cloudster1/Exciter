-- +goose Up
-- The organization's record of who did what to it, written in the same transaction as the act. It is only
-- ever added to; entries leave through audit_log_prune, never younger than a day, or with their organization.
CREATE TABLE audit_log (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    actor_user_id uuid REFERENCES app_user(id) ON DELETE SET NULL,
    action        text NOT NULL CHECK (btrim(action) <> ''),
    target_type   text NOT NULL,
    target_id     uuid,
    data          jsonb NOT NULL DEFAULT '{}'::jsonb,
    ip            inet,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT audit_log_data_object CHECK (jsonb_typeof(data) = 'object')
);

-- Administrators page through it newest first, by (time, id), narrowed by action, actor or target, so each
-- filter gets an index that ends in the keyset.
CREATE INDEX audit_log_org_created_idx ON audit_log (org_id, created_at DESC, id DESC);
CREATE INDEX audit_log_org_action_idx ON audit_log (org_id, action, created_at DESC, id DESC);
CREATE INDEX audit_log_org_actor_idx ON audit_log (org_id, actor_user_id, created_at DESC, id DESC);
CREATE INDEX audit_log_target_idx ON audit_log (org_id, target_id, created_at DESC, id DESC);

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log FORCE  ROW LEVEL SECURITY;

CREATE POLICY audit_log_tenant_isolation ON audit_log
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY audit_log_admin_bypass ON audit_log TO exciter_admin USING (true) WITH CHECK (true);

-- Only the organization's owners and administrators read its record; #12 moves this onto the permission
-- functions. The subquery runs once per statement instead of once per row the scan meets.
CREATE POLICY audit_log_admins_read ON audit_log AS RESTRICTIVE FOR SELECT TO exciter_app
    USING ((SELECT EXISTS (
        SELECT 1 FROM org_member m
        WHERE m.org_id = current_org_id() AND m.user_id = current_actor_id() AND m.org_role IN ('owner', 'admin'))));

-- An entry names the person the transaction acts for, or nobody when the act is the system's, so the app role
-- cannot write an act down to someone else.
CREATE POLICY audit_log_actor_is_caller ON audit_log AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (actor_user_id IS NULL OR actor_user_id = current_actor_id());

-- A record that can be rewritten records nothing.
REVOKE UPDATE, DELETE ON audit_log FROM exciter_app, exciter_admin;
GRANT SELECT, INSERT ON audit_log TO exciter_app, exciter_admin;

-- +goose StatementBegin
-- Retention: deletes the current organization's entries older than cutoff and says how many went. It is the
-- only way an entry goes before its organization does, and it never takes one younger than a day.
CREATE FUNCTION audit_log_prune(cutoff timestamptz) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    gone bigint;
BEGIN
    IF current_org_id() IS NULL THEN
        RAISE EXCEPTION 'audit_log_prune runs inside one organization' USING ERRCODE = '42501';
    END IF;
    IF cutoff > now() - interval '1 day' THEN
        RAISE EXCEPTION 'the audit log keeps every entry for at least a day' USING ERRCODE = '22023';
    END IF;
    DELETE FROM audit_log WHERE org_id = current_org_id() AND created_at < cutoff;
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$$;
-- +goose StatementEnd

-- The schema's default privileges hand every new function to both roles.
REVOKE EXECUTE ON FUNCTION audit_log_prune(timestamptz) FROM PUBLIC, exciter_app;
GRANT EXECUTE ON FUNCTION audit_log_prune(timestamptz) TO exciter_admin;

-- +goose Down
DROP FUNCTION IF EXISTS audit_log_prune(timestamptz);
DROP TABLE IF EXISTS audit_log;
