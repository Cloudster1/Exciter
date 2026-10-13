-- +goose Up
-- A sign-in lands in the installation (#85): there is no switching, so nothing asks where a session reaches, and
-- the organization a proof was made for goes with it. The organization a session acts in stays, written once
-- when it opens, while the integration suite still signs the same people in to organizations of its own (#90).
DROP TRIGGER session_stays_home ON user_session;
DROP FUNCTION session_stays_home();
DROP FUNCTION session_reaches(text, uuid, uuid, uuid);
ALTER TABLE user_session DROP COLUMN proof_org_id;

-- A session keeps how it was proven and where it opened; the organization going, which nulls the column, is the
-- one way that changes.
-- +goose StatementBegin
CREATE FUNCTION session_keeps_its_opening() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.proof IS DISTINCT FROM OLD.proof THEN
        RAISE EXCEPTION 'a session keeps the proof it was opened with' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.current_org_id IS NOT NULL AND NEW.current_org_id IS DISTINCT FROM OLD.current_org_id THEN
        RAISE EXCEPTION 'a session keeps the organization it was opened in' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER session_keeps_its_opening BEFORE UPDATE OF current_org_id, proof ON user_session
    FOR EACH ROW EXECUTE FUNCTION session_keeps_its_opening();

-- +goose Down
DROP TRIGGER session_keeps_its_opening ON user_session;
DROP FUNCTION session_keeps_its_opening();
ALTER TABLE user_session ADD COLUMN proof_org_id uuid REFERENCES org(id) ON DELETE SET NULL;
UPDATE user_session SET proof_org_id = current_org_id;

-- +goose StatementBegin
CREATE FUNCTION session_reaches(proof text, proof_org uuid, person uuid, target uuid) RETURNS boolean
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

-- +goose StatementBegin
CREATE FUNCTION session_stays_home() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.proof IS DISTINCT FROM OLD.proof THEN
        RAISE EXCEPTION 'a session keeps the proof it was opened with' USING ERRCODE = 'check_violation';
    END IF;
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
