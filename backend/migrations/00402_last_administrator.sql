-- +goose Up
-- People belong to the installation (#86): letting somebody go switches their account off rather than taking a
-- membership away, and the installation keeps an active administrator, owner or admin, rather than an owner.
DROP TRIGGER org_member_owner_stays ON org_member;
DROP FUNCTION org_member_owner_stays();

-- Whether anybody but the person leaving still administers the organization with an account switched on. The lock
-- makes two administrators switching each other off take turns, so the second sees the first one gone.
-- +goose StatementBegin
CREATE FUNCTION administered_without(org uuid, leaving uuid) RETURNS boolean
    LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('administered_without'));
    RETURN EXISTS (SELECT 1 FROM org_member m JOIN app_user u ON u.id = m.user_id
                   WHERE m.org_id = org AND m.user_id <> leaving AND m.org_role IN ('owner', 'admin') AND u.is_active);
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION org_member_keeps_an_administrator() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.org_role IN ('owner', 'admin') AND (TG_OP = 'DELETE' OR NEW.org_role NOT IN ('owner', 'admin'))
       AND EXISTS (SELECT 1 FROM org WHERE id = OLD.org_id)
       AND EXISTS (SELECT 1 FROM app_user WHERE id = OLD.user_id AND is_active)
       AND NOT administered_without(OLD.org_id, OLD.user_id) THEN
        RAISE EXCEPTION 'the installation keeps its last active administrator' USING ERRCODE = 'check_violation';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER org_member_keeps_an_administrator BEFORE UPDATE OF org_role OR DELETE ON org_member
    FOR EACH ROW EXECUTE FUNCTION org_member_keeps_an_administrator();

-- +goose StatementBegin
CREATE FUNCTION app_user_keeps_an_administrator() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.is_active AND NOT NEW.is_active AND EXISTS (
        SELECT 1 FROM org_member m JOIN org o ON o.id = m.org_id
        WHERE m.user_id = NEW.id AND m.org_role IN ('owner', 'admin') AND o.archived_at IS NULL
          AND NOT administered_without(m.org_id, NEW.id)) THEN
        RAISE EXCEPTION 'the last active administrator cannot be switched off' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER app_user_keeps_an_administrator BEFORE UPDATE OF is_active ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_keeps_an_administrator();

-- Through the app role only an administrator changes an account, which is switching it off or on; a person's own
-- settings and a provider's sign-in go through the admin role. Nobody leaves through the app role either.
CREATE POLICY app_user_admins_change ON app_user AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (current_actor_administers()) WITH CHECK (current_actor_administers());
DROP POLICY org_member_admins_remove ON org_member;
REVOKE DELETE ON org_member FROM exciter_app;

-- Owners are made by the bootstrap, through the admin role, and the app role does not unmake one: an owner may be
-- switched off by another owner, but stays an owner.
-- +goose StatementBegin
CREATE FUNCTION org_member_owner_keeps_standing() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_user = 'exciter_app' AND OLD.org_role = 'owner' AND NEW.org_role <> 'owner' THEN
        RAISE EXCEPTION 'an owner''s role is not changed here' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER org_member_owner_keeps_standing BEFORE UPDATE OF org_role ON org_member
    FOR EACH ROW EXECUTE FUNCTION org_member_owner_keeps_standing();

-- +goose Down
DROP TRIGGER org_member_owner_keeps_standing ON org_member;
DROP FUNCTION org_member_owner_keeps_standing();
GRANT DELETE ON org_member TO exciter_app;
CREATE POLICY org_member_admins_remove ON org_member AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (current_actor_administers());
DROP POLICY app_user_admins_change ON app_user;

DROP TRIGGER app_user_keeps_an_administrator ON app_user;
DROP FUNCTION app_user_keeps_an_administrator();
DROP TRIGGER org_member_keeps_an_administrator ON org_member;
DROP FUNCTION org_member_keeps_an_administrator();
DROP FUNCTION administered_without(uuid, uuid);

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
