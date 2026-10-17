-- +goose Up
-- Signed baselines (#205), as docs/api-contract-m8.md agrees: a project's administrators name who signs its baselines
-- and how many signatures release one; the first signature freezes that rule on the baseline, as a review freezes its
-- approvers, and locks it. A signature is #46's: a meaning, a fresh proof of identity it spends, and the hash of what
-- the baseline holds. A signed baseline keeps its name and is never deleted.

CREATE TABLE baseline_signing_rule (
    project_id uuid PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    -- review.MinRequired to review.MaxRequired.
    required   integer NOT NULL CHECK (required BETWEEN 1 AND 10),
    updated_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- People and groups, as an approval rule names them.
CREATE TABLE baseline_signing_rule_signer (
    project_id   uuid NOT NULL REFERENCES baseline_signing_rule(project_id) ON DELETE CASCADE,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
    user_id      uuid REFERENCES app_user(id) ON DELETE CASCADE,
    group_id     uuid REFERENCES groups(id) ON DELETE CASCADE,
    CONSTRAINT baseline_signing_rule_signer_whole CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
        (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL))
);
CREATE UNIQUE INDEX baseline_signing_rule_signer_idx ON baseline_signing_rule_signer (project_id, subject_type, user_id, group_id)
    NULLS NOT DISTINCT;

-- The rule as the first signature found it, which the baseline keeps.
CREATE TABLE baseline_signing (
    baseline_id uuid PRIMARY KEY REFERENCES baseline(id) ON DELETE CASCADE,
    required    integer NOT NULL CHECK (required BETWEEN 1 AND 10),
    frozen_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE baseline_signer (
    baseline_id  uuid NOT NULL REFERENCES baseline_signing(baseline_id) ON DELETE CASCADE,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
    user_id      uuid REFERENCES app_user(id) ON DELETE CASCADE,
    group_id     uuid REFERENCES groups(id) ON DELETE CASCADE,
    CONSTRAINT baseline_signer_whole CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
        (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL))
);
CREATE UNIQUE INDEX baseline_signer_idx ON baseline_signer (baseline_id, subject_type, user_id, group_id) NULLS NOT DISTINCT;

-- A signature, as it was given; it never changes, and each signer gives one.
CREATE TABLE baseline_signature (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    baseline_id         uuid NOT NULL REFERENCES baseline(id) ON DELETE CASCADE,
    content_hash        text NOT NULL,
    signer_id           uuid NOT NULL REFERENCES app_user(id),
    meaning             text NOT NULL,
    method              text NOT NULL CHECK (method IN ('password', 'oidc')),
    authenticated_at    timestamptz NOT NULL,
    session_id          uuid NOT NULL,
    reauthentication_id uuid NOT NULL UNIQUE,
    signed_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT baseline_signature_meaning CHECK (btrim(meaning) <> '' AND char_length(meaning) <= 500),
    CONSTRAINT baseline_signature_once UNIQUE (baseline_id, signer_id)
);

-- +goose StatementBegin
-- The SHA-256 of what a baseline holds, one line per row in the form the contract gives; baseline.ContentHash writes
-- the same in Go.
CREATE FUNCTION baseline_content_hash(bid uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT encode(sha256(convert_to(
        (SELECT format(E'baseline %s %s %s\n', b.id, b.project_id, COALESCE(b.root_id::text, '-')) FROM baseline b WHERE b.id = bid)
        || COALESCE((SELECT string_agg(format(E'item %s %s %s %s\n', i.requirement_id, i.revision, COALESCE(v.hash, '-'),
                                              array_to_string(i.path, '.')), '' ORDER BY i.path, i.requirement_id)
                     FROM baseline_item i
                     LEFT JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
                     WHERE i.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'link %s %s %s\n', l.type_id, l.source_id, l.target_id), ''
                                       ORDER BY l.type_id, l.source_id, l.target_id)
                     FROM baseline_link l WHERE l.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'external %s\n', e.link_id), '' ORDER BY e.link_id)
                     FROM baseline_external_link e WHERE e.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'page %s\n', g.link_id), '' ORDER BY g.link_id)
                     FROM baseline_page_link g WHERE g.baseline_id = bid), ''),
        'UTF8')), 'hex')
$$;

-- What the hash covers, row by row, for a reader of the baseline's project, who may not read a trashed requirement's
-- revisions; baseline.ContentHash orders and writes them as baseline_content_hash does.
CREATE FUNCTION baseline_content_rows(bid uuid)
    RETURNS TABLE (kind text, a uuid, b uuid, c uuid, revision integer, hash text, path integer[])
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH readable AS (
        SELECT 1 FROM baseline x WHERE x.id = bid AND perm_project_holds(x.project_id, current_actor_id(), 'view'))
    SELECT 'item', i.requirement_id, NULL::uuid, NULL::uuid, i.revision, v.hash, i.path
    FROM baseline_item i LEFT JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
    WHERE i.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'link', l.type_id, l.source_id, l.target_id, NULL, NULL, NULL
    FROM baseline_link l WHERE l.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'external', e.link_id, NULL, NULL, NULL, NULL, NULL
    FROM baseline_external_link e WHERE e.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'page', g.link_id, NULL, NULL, NULL, NULL, NULL
    FROM baseline_page_link g WHERE g.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
$$;

-- Whether a project's signing rule names the actor, in person or through a group.
CREATE FUNCTION baseline_signing_rule_names(pid uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM baseline_signing_rule_signer s
        WHERE s.project_id = pid
          AND (s.user_id = actor OR EXISTS (SELECT 1 FROM group_member m WHERE m.group_id = s.group_id AND m.user_id = actor)))
$$;

-- Whether the actor is one of the signers frozen on a baseline, in person or through a group.
CREATE FUNCTION baseline_signer_named(bid uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM baseline_signer s
        WHERE s.baseline_id = bid
          AND (s.user_id = actor OR EXISTS (SELECT 1 FROM group_member m WHERE m.group_id = s.group_id AND m.user_id = actor)))
$$;

-- A signature is given by a signer in their own name, of an open project's baseline they may read, spending a fresh,
-- unused re-authentication of the session it names, over the baseline's content as it is. The first freezes the
-- project's rule on the baseline and locks it.
CREATE FUNCTION baseline_signature_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    b baseline%ROWTYPE;
    proof reauthentication%ROWTYPE;
BEGIN
    SELECT * INTO b FROM baseline WHERE id = NEW.baseline_id FOR UPDATE;
    IF b.id IS NULL OR NEW.signer_id IS DISTINCT FROM current_actor_id() OR NOT perm_project_holds(b.project_id, NEW.signer_id, 'view')
       OR EXISTS (SELECT 1 FROM project p WHERE p.id = b.project_id AND p.archived_at IS NOT NULL) THEN
        RAISE EXCEPTION 'baseline_signature_not_theirs' USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_signature_not_theirs',
            DETAIL = 'A signature is given by a signer of an open project''s baseline, in their own name.';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM baseline_signing WHERE baseline_id = b.id) THEN
        IF NOT EXISTS (SELECT 1 FROM baseline_signing_rule WHERE project_id = b.project_id) THEN
            RAISE EXCEPTION 'baseline_signature_no_rule' USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_signature_no_rule',
                DETAIL = 'Nobody signs this project''s baselines.';
        END IF;
        INSERT INTO baseline_signing (baseline_id, required)
        SELECT b.id, required FROM baseline_signing_rule WHERE project_id = b.project_id;
        INSERT INTO baseline_signer (baseline_id, subject_type, user_id, group_id)
        SELECT b.id, subject_type, user_id, group_id FROM baseline_signing_rule_signer WHERE project_id = b.project_id;
    END IF;
    IF NOT baseline_signer_named(b.id, NEW.signer_id) THEN
        RAISE EXCEPTION 'baseline_signature_not_theirs' USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_signature_not_theirs',
            DETAIL = 'A signature is given by one of the signers the baseline froze.';
    END IF;
    SELECT * INTO proof FROM reauthentication WHERE id = NEW.reauthentication_id FOR UPDATE;
    IF proof.id IS NULL OR proof.user_id <> NEW.signer_id OR proof.used_at IS NOT NULL OR proof.expires_at <= now()
       OR proof.session_id <> NEW.session_id OR proof.method <> NEW.method OR proof.authenticated_at <> NEW.authenticated_at THEN
        RAISE EXCEPTION 'baseline_signature_unproven' USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_signature_unproven',
            DETAIL = 'A signature spends a fresh, unused re-authentication of its signer, given in the session it names.';
    END IF;
    IF NEW.content_hash IS DISTINCT FROM baseline_content_hash(b.id) THEN
        RAISE EXCEPTION 'baseline_signature_wrong_content' USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_signature_wrong_content',
            DETAIL = 'A signature signs the baseline by the hash of what it holds.';
    END IF;
    UPDATE reauthentication SET used_at = now() WHERE id = proof.id;
    IF b.locked_at IS NULL THEN
        UPDATE baseline SET locked_at = now(), locked_by = NEW.signer_id WHERE id = b.id;
    END IF;
    RETURN NEW;
END;
$$;

-- A baseline keeps its project, root and transaction; a locked one, and a signed one above all, keeps its name and
-- description and is never deleted, unless its project goes, which cascades from deeper down.
CREATE OR REPLACE FUNCTION baseline_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF pg_trigger_depth() = 1 AND EXISTS (SELECT 1 FROM baseline_signature s WHERE s.baseline_id = OLD.id) THEN
            RAISE EXCEPTION 'a signed baseline is never deleted'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_signed';
        END IF;
        IF OLD.locked_at IS NOT NULL AND pg_trigger_depth() = 1 THEN
            RAISE EXCEPTION 'a locked baseline is never deleted'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_locked';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.project_id <> OLD.project_id OR NEW.root_id IS DISTINCT FROM OLD.root_id
       OR NEW.root_number IS DISTINCT FROM OLD.root_number OR NEW.taken_xact <> OLD.taken_xact
       OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'a baseline keeps what it was taken of'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_fixed';
    END IF;
    IF EXISTS (SELECT 1 FROM baseline_signature s WHERE s.baseline_id = OLD.id) THEN
        RAISE EXCEPTION 'a signed baseline does not change'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_signed';
    END IF;
    IF OLD.locked_at IS NOT NULL THEN
        RAISE EXCEPTION 'a locked baseline does not change'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_locked';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER baseline_signature_guard BEFORE INSERT ON baseline_signature
    FOR EACH ROW EXECUTE FUNCTION baseline_signature_guard();

ALTER TABLE baseline_signing_rule        ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline_signing_rule        FORCE  ROW LEVEL SECURITY;
ALTER TABLE baseline_signing_rule_signer ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline_signing_rule_signer FORCE  ROW LEVEL SECURITY;
ALTER TABLE baseline_signing             ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline_signing             FORCE  ROW LEVEL SECURITY;
ALTER TABLE baseline_signer              ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline_signer              FORCE  ROW LEVEL SECURITY;
ALTER TABLE baseline_signature           ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline_signature           FORCE  ROW LEVEL SECURITY;

CREATE POLICY baseline_signing_rule_installation ON baseline_signing_rule USING (true) WITH CHECK (true);
CREATE POLICY baseline_signing_rule_signer_installation ON baseline_signing_rule_signer USING (true) WITH CHECK (true);
CREATE POLICY baseline_signing_installation ON baseline_signing USING (true) WITH CHECK (true);
CREATE POLICY baseline_signer_installation ON baseline_signer USING (true) WITH CHECK (true);
CREATE POLICY baseline_signature_installation ON baseline_signature USING (true) WITH CHECK (true);

-- A project's readers read who signs its baselines; its administrators set that while it is open.
CREATE POLICY baseline_signing_rule_readers ON baseline_signing_rule AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY baseline_signing_rule_setters ON baseline_signing_rule AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer')
                AND EXISTS (SELECT 1 FROM project p WHERE p.id = project_id AND p.archived_at IS NULL));
CREATE POLICY baseline_signing_rule_changers ON baseline_signing_rule AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer')) WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer')
                AND EXISTS (SELECT 1 FROM project p WHERE p.id = project_id AND p.archived_at IS NULL));
CREATE POLICY baseline_signing_rule_removers ON baseline_signing_rule AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY baseline_signing_rule_signer_readers ON baseline_signing_rule_signer AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY baseline_signing_rule_signer_setters ON baseline_signing_rule_signer AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY baseline_signing_rule_signer_changers ON baseline_signing_rule_signer AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer')) WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY baseline_signing_rule_signer_removers ON baseline_signing_rule_signer AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer'));

-- What a baseline froze and its signatures show with it; the subqueries read as the reader. Only the guard writes
-- what is frozen, and a signer only their own signature.
CREATE POLICY baseline_signing_readers ON baseline_signing AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id));
CREATE POLICY baseline_signer_readers ON baseline_signer AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id));
CREATE POLICY baseline_signature_readers ON baseline_signature AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id));
CREATE POLICY baseline_signature_signers ON baseline_signature AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (signer_id = current_actor_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON baseline_signing_rule, baseline_signing_rule_signer, baseline_signing, baseline_signer,
    baseline_signature TO exciter_admin;
REVOKE ALL ON baseline_signing_rule, baseline_signing_rule_signer, baseline_signing, baseline_signer, baseline_signature FROM exciter_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON baseline_signing_rule, baseline_signing_rule_signer TO exciter_app;
GRANT SELECT ON baseline_signing, baseline_signer TO exciter_app;
-- A signature never changes.
GRANT SELECT, INSERT ON baseline_signature TO exciter_app;

-- +goose Down
DROP TRIGGER IF EXISTS baseline_signature_guard ON baseline_signature;
DROP TABLE IF EXISTS baseline_signature;
DROP TABLE IF EXISTS baseline_signer;
DROP TABLE IF EXISTS baseline_signing;
DROP TABLE IF EXISTS baseline_signing_rule_signer;
DROP TABLE IF EXISTS baseline_signing_rule;
DROP FUNCTION IF EXISTS baseline_signature_guard();
DROP FUNCTION IF EXISTS baseline_signer_named(uuid, uuid);
DROP FUNCTION IF EXISTS baseline_signing_rule_names(uuid, uuid);
DROP FUNCTION IF EXISTS baseline_content_rows(uuid);
DROP FUNCTION IF EXISTS baseline_content_hash(uuid);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION baseline_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.locked_at IS NOT NULL AND pg_trigger_depth() = 1 THEN
            RAISE EXCEPTION 'a locked baseline is never deleted'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_locked';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.project_id <> OLD.project_id OR NEW.root_id IS DISTINCT FROM OLD.root_id
       OR NEW.root_number IS DISTINCT FROM OLD.root_number OR NEW.taken_xact <> OLD.taken_xact
       OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'a baseline keeps what it was taken of'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_fixed';
    END IF;
    IF OLD.locked_at IS NOT NULL THEN
        RAISE EXCEPTION 'a locked baseline does not change'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_locked';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
