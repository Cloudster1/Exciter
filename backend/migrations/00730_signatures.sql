-- +goose Up
-- Approvals as electronic signatures (#46). In a project whose administrators turned the setting on, deciding on a
-- revision is signing it: the reviewer proves who they are again, and the signature keeps what they signed and how.

-- One proof of identity given again by a signed-in person, good for one signature only, and only briefly: the
-- password typed now, or a fresh sign-in at the identity provider. It belongs to the session it was given in.
CREATE TABLE reauthentication (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id          uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    session_id       uuid NOT NULL REFERENCES user_session (id) ON DELETE CASCADE,
    method           text NOT NULL CHECK (method IN ('password', 'oidc')),
    authenticated_at timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL,
    used_at          timestamptz,
    CONSTRAINT reauthentication_is_brief CHECK (expires_at <= created_at + interval '10 minutes'),
    CONSTRAINT reauthentication_is_fresh CHECK (authenticated_at BETWEEN created_at - interval '10 minutes' AND created_at + interval '1 minute')
);

CREATE INDEX reauthentication_user_idx ON reauthentication (user_id, created_at DESC);

-- A wrong password given to sign, so a run of them can be refused for a while.
CREATE TABLE signature_failure (
    user_id   uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    failed_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX signature_failure_user_idx ON signature_failure (user_id, failed_at DESC);

-- A sign-in at the identity provider is either one that opens a session, or one that proves again who an open
-- session's person is, for a signature: that one names the person and the session, and when it was asked for.
ALTER TABLE oidc_login
    ADD COLUMN purpose      text NOT NULL DEFAULT 'signin' CHECK (purpose IN ('signin', 'reauth')),
    ADD COLUMN user_id      uuid REFERENCES app_user (id) ON DELETE CASCADE,
    ADD COLUMN session_id   uuid REFERENCES user_session (id) ON DELETE CASCADE,
    ADD COLUMN requested_at timestamptz NOT NULL DEFAULT now(),
    ADD CONSTRAINT oidc_login_reauth_names_its_session CHECK ((purpose = 'reauth') = (user_id IS NOT NULL AND session_id IS NOT NULL));

-- A signature, as it was given; it never changes. It keeps the meaning the signer confirmed, the revision and the
-- hash of its normative content, and how and when the signer proved who they are, in which session.
CREATE TABLE review_signature (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    review_id           uuid NOT NULL REFERENCES requirement_review (id) ON DELETE CASCADE,
    requirement_id      uuid NOT NULL,
    revision            integer NOT NULL,
    content_hash        text NOT NULL,
    signer_id           uuid NOT NULL REFERENCES app_user (id),
    decision            text NOT NULL CHECK (decision IN ('approve', 'reject')),
    meaning             text NOT NULL,
    method              text NOT NULL CHECK (method IN ('password', 'oidc')),
    authenticated_at    timestamptz NOT NULL,
    session_id          uuid NOT NULL,
    reauthentication_id uuid NOT NULL UNIQUE,
    signed_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT review_signature_meaning CHECK (btrim(meaning) <> '' AND char_length(meaning) <= 500)
);

CREATE INDEX review_signature_review_idx ON review_signature (review_id, signer_id, signed_at DESC);

-- A signature spends a re-authentication of its signer that is unused, unexpired and given in the session it names,
-- and signs the revision under review as it stands, by somebody who may decide on it.
-- +goose StatementBegin
CREATE FUNCTION review_signature_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    proof reauthentication%ROWTYPE;
    review requirement_review%ROWTYPE;
BEGIN
    IF NEW.signer_id IS DISTINCT FROM current_actor_id() OR NOT review_decidable(NEW.review_id, NEW.signer_id) THEN
        RAISE EXCEPTION 'review_signature_not_theirs' USING ERRCODE = 'check_violation',
            DETAIL = 'A signature is given by an approver of a pending review, in their own name.';
    END IF;
    SELECT * INTO proof FROM reauthentication WHERE id = NEW.reauthentication_id FOR UPDATE;
    IF proof.id IS NULL OR proof.user_id <> NEW.signer_id OR proof.used_at IS NOT NULL OR proof.expires_at <= now()
       OR proof.session_id <> NEW.session_id OR proof.method <> NEW.method OR proof.authenticated_at <> NEW.authenticated_at THEN
        RAISE EXCEPTION 'review_signature_unproven' USING ERRCODE = 'check_violation',
            DETAIL = 'A signature spends a fresh, unused re-authentication of its signer, given in the session it names.';
    END IF;
    SELECT * INTO review FROM requirement_review WHERE id = NEW.review_id;
    IF NEW.requirement_id <> review.requirement_id OR NEW.revision <> review.revision
       OR NEW.content_hash IS DISTINCT FROM (SELECT hash FROM requirement_revision WHERE requirement_id = review.requirement_id AND number = review.revision) THEN
        RAISE EXCEPTION 'review_signature_wrong_revision' USING ERRCODE = 'check_violation',
            DETAIL = 'A signature signs the revision under review, by the hash of its content.';
    END IF;
    UPDATE reauthentication SET used_at = now() WHERE id = proof.id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER review_signature_guard BEFORE INSERT ON review_signature
    FOR EACH ROW EXECUTE FUNCTION review_signature_guard();

-- In a project whose approvals are signatures, a decision stands only on a signature of the same reviewer and the same
-- decision, given in the same transaction.
-- +goose StatementBegin
CREATE FUNCTION review_decision_signed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM requirement_review v JOIN requirement r ON r.id = v.requirement_id JOIN project p ON p.id = r.project_id
               WHERE v.id = NEW.review_id AND COALESCE((p.settings ->> 'signedApprovals')::boolean, false))
       AND NOT EXISTS (SELECT 1 FROM review_signature s WHERE s.review_id = NEW.review_id AND s.signer_id = NEW.reviewer_id
                       AND s.decision = NEW.decision AND s.signed_at = now()) THEN
        RAISE EXCEPTION 'review_decision_unsigned' USING ERRCODE = 'check_violation',
            DETAIL = 'In this project a decision is an electronic signature, which the reviewer gives first.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER review_decision_signed BEFORE INSERT OR UPDATE ON review_decision
    FOR EACH ROW EXECUTE FUNCTION review_decision_signed();

ALTER TABLE reauthentication  ENABLE ROW LEVEL SECURITY;
ALTER TABLE reauthentication  FORCE  ROW LEVEL SECURITY;
ALTER TABLE signature_failure ENABLE ROW LEVEL SECURITY;
ALTER TABLE signature_failure FORCE  ROW LEVEL SECURITY;
ALTER TABLE review_signature  ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_signature  FORCE  ROW LEVEL SECURITY;

CREATE POLICY reauthentication_installation ON reauthentication USING (true) WITH CHECK (true);
CREATE POLICY signature_failure_installation ON signature_failure USING (true) WITH CHECK (true);
CREATE POLICY review_signature_installation ON review_signature USING (true) WITH CHECK (true);

-- A person's re-authentications and wrong passwords are theirs alone. A password typed now is recorded by the api as
-- the person; one at the identity provider is recorded by the callback, which proves it first.
CREATE POLICY reauthentication_own ON reauthentication AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (user_id = current_actor_id());
CREATE POLICY reauthentication_given ON reauthentication AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (user_id = current_actor_id() AND method = 'password' AND used_at IS NULL);
CREATE POLICY reauthentication_spent ON reauthentication AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (user_id = current_actor_id()) WITH CHECK (user_id = current_actor_id());
CREATE POLICY signature_failure_own ON signature_failure AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (user_id = current_actor_id());
CREATE POLICY signature_failure_noted ON signature_failure AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (user_id = current_actor_id());

-- A signature is read with its review and given by its signer.
CREATE POLICY review_signature_readers ON review_signature AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (review_viewable(review_id, current_actor_id()));
CREATE POLICY review_signature_signers ON review_signature AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (signer_id = current_actor_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON reauthentication, signature_failure, review_signature TO exciter_admin;
REVOKE ALL ON reauthentication, signature_failure, review_signature FROM exciter_app;
GRANT SELECT, INSERT ON reauthentication, signature_failure, review_signature TO exciter_app;
-- Only spending a re-authentication changes it, and a signature never changes.
GRANT UPDATE (used_at) ON reauthentication TO exciter_app;

-- +goose Down
DROP TABLE IF EXISTS review_signature;
DROP FUNCTION IF EXISTS review_signature_guard();
DROP TRIGGER IF EXISTS review_decision_signed ON review_decision;
DROP FUNCTION IF EXISTS review_decision_signed();
ALTER TABLE oidc_login DROP CONSTRAINT IF EXISTS oidc_login_reauth_names_its_session,
    DROP COLUMN IF EXISTS requested_at, DROP COLUMN IF EXISTS session_id, DROP COLUMN IF EXISTS user_id, DROP COLUMN IF EXISTS purpose;
DROP TABLE IF EXISTS signature_failure;
DROP TABLE IF EXISTS reauthentication;
