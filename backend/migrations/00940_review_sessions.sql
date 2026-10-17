-- +goose Up
-- Review sessions (#204), as docs/api-contract-m8.md agrees: a moderator sends a section, a baseline's requirements or
-- a query's to reviewers and approvers in rounds, and closes it with a report and a baseline of what was approved.
-- Each requirement's review is a review of #35 whose approvers are the session's, so a decision, its signature and the
-- "Approved" condition hold as they are; the database holds a session's reviews to it.

CREATE TABLE review_session (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id     uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    -- review.MaxSessionTitleLength and review.MaxCommentLength.
    title          text NOT NULL CHECK (btrim(title) <> '' AND char_length(title) <= 100),
    description    text NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    scope_kind     text NOT NULL CHECK (scope_kind IN ('section', 'baseline', 'query')),
    scope_root     uuid,
    scope_baseline uuid,
    scope_query    text NOT NULL DEFAULT '',
    since_base     text CHECK (since_base IN ('approved', 'baseline')),
    since_baseline uuid,
    moderator_id   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    deadline       timestamptz,
    quorum         integer NOT NULL CHECK (quorum BETWEEN 1 AND 100),
    state          text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'closed')),
    -- review.MaxSessionRounds.
    round          integer NOT NULL DEFAULT 1 CHECK (round BETWEEN 1 AND 20),
    baseline_id    uuid REFERENCES baseline(id) ON DELETE SET NULL,
    -- What the scope held that the session left out, [{key, reason}].
    excluded       jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(excluded) = 'array'),
    reminded_at    timestamptz,
    opened_at      timestamptz NOT NULL DEFAULT now(),
    closed_at      timestamptz,
    CONSTRAINT review_session_closed CHECK ((state = 'closed') = (closed_at IS NOT NULL))
);
CREATE INDEX review_session_project_idx ON review_session (project_id, opened_at DESC);
CREATE INDEX review_session_due_idx ON review_session (deadline) WHERE state = 'open' AND reminded_at IS NULL;

CREATE TABLE review_session_participant (
    session_id   uuid NOT NULL REFERENCES review_session(id) ON DELETE CASCADE,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
    user_id      uuid REFERENCES app_user(id) ON DELETE CASCADE,
    group_id     uuid REFERENCES groups(id) ON DELETE CASCADE,
    role         text NOT NULL CHECK (role IN ('reviewer', 'approver')),
    CONSTRAINT review_session_participant_whole CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
        (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL))
);
CREATE UNIQUE INDEX review_session_participant_idx ON review_session_participant (session_id, subject_type, user_id, group_id) NULLS NOT DISTINCT;

CREATE TABLE review_session_round (
    session_id uuid NOT NULL REFERENCES review_session(id) ON DELETE CASCADE,
    number     integer NOT NULL CHECK (number BETWEEN 1 AND 20),
    comment    text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
    started_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, number)
);

ALTER TABLE requirement_review ADD COLUMN session_id uuid REFERENCES review_session(id) ON DELETE SET NULL;
CREATE INDEX requirement_review_session_idx ON requirement_review (session_id) WHERE session_id IS NOT NULL;
-- A session's review may need as many approvals as its approvers reach, review.MaxSessionApprovers; a rule's stays
-- review.MaxRequired.
ALTER TABLE requirement_review DROP CONSTRAINT requirement_review_required_check,
    ADD CONSTRAINT requirement_review_required CHECK (required BETWEEN 1 AND 50 AND (session_id IS NOT NULL OR required <= 10));

-- A requirement pinned in a round, at a revision, with the review that round asked of it.
CREATE TABLE review_session_item (
    session_id     uuid NOT NULL REFERENCES review_session(id) ON DELETE CASCADE,
    requirement_id uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    round          integer NOT NULL,
    revision       integer NOT NULL,
    review_id      uuid NOT NULL REFERENCES requirement_review(id) ON DELETE CASCADE,
    PRIMARY KEY (session_id, requirement_id, round),
    FOREIGN KEY (session_id, round) REFERENCES review_session_round (session_id, number) ON DELETE CASCADE
);

-- A baseline a session took of what it approved.
ALTER TABLE baseline ADD COLUMN session_id uuid REFERENCES review_session(id) ON DELETE SET NULL;

-- A session's notifications name it rather than a requirement.
ALTER TABLE notification ALTER COLUMN requirement_id DROP NOT NULL,
    ADD COLUMN session_id uuid REFERENCES review_session(id) ON DELETE CASCADE,
    ADD CONSTRAINT notification_about_something CHECK (requirement_id IS NOT NULL OR session_id IS NOT NULL),
    DROP CONSTRAINT notification_kind_check,
    ADD CONSTRAINT notification_kind_check CHECK (kind IN ('mentioned', 'review_requested', 'reviewed', 'suspect', 'replied',
        'commented', 'resolved', 'revised', 'transitioned', 'source_revised', 'session_invited', 'session_round',
        'session_reminder', 'session_closed'));

-- +goose StatementBegin
-- Whether the actor moderates a session: whoever opened it, or one of its project's administrators.
CREATE FUNCTION review_session_moderated(sid uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM review_session s
        WHERE s.id = sid AND (s.moderator_id = actor OR perm_project_holds(s.project_id, actor, 'administer')))
$$;

-- Whether the actor takes part in a session, in person or through a group, in a role or in any.
CREATE FUNCTION review_session_takes_part(sid uuid, actor uuid, as_role text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM review_session_participant p
        WHERE p.session_id = sid AND (as_role IS NULL OR p.role = as_role)
          AND (p.user_id = actor OR EXISTS (SELECT 1 FROM group_member m WHERE m.group_id = p.group_id AND m.user_id = actor)))
$$;

-- Pins a requirement of an open session in its round at its current revision, asking its review in the moderator's
-- name of the session's approvers, with the share of them the quorum needs; the revision's author, who never approves
-- it, is not counted.
CREATE FUNCTION review_session_pin(sid uuid, rid uuid) RETURNS uuid
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    s review_session%ROWTYPE;
    r requirement%ROWTYPE;
    author uuid;
    reached integer;
    review uuid;
BEGIN
    SELECT * INTO s FROM review_session WHERE id = sid FOR UPDATE;
    IF s.id IS NULL OR s.state <> 'open' OR NOT review_session_moderated(sid, actor) THEN
        RAISE EXCEPTION 'only the moderator of an open session pins its requirements'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'review_session_pin';
    END IF;
    SELECT * INTO r FROM requirement WHERE id = rid;
    IF r.id IS NULL OR r.project_id <> s.project_id OR r.kind <> 'requirement' OR r.trashed_at IS NOT NULL THEN
        RAISE EXCEPTION 'a session pins requirements of its own project out of the trash'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'review_session_pin';
    END IF;
    SELECT created_by INTO author FROM requirement_revision WHERE requirement_id = rid AND number = r.revision;
    SELECT count(DISTINCT person) INTO reached FROM (
        SELECT p.user_id AS person FROM review_session_participant p WHERE p.session_id = sid AND p.role = 'approver' AND p.user_id IS NOT NULL
        UNION
        SELECT m.user_id FROM review_session_participant p JOIN group_member m ON m.group_id = p.group_id
        WHERE p.session_id = sid AND p.role = 'approver'
    ) people WHERE person IS DISTINCT FROM author;
    INSERT INTO requirement_review (requirement_id, revision, requested_by, comment, required, author_may_approve, session_id)
    VALUES (rid, r.revision, COALESCE(s.moderator_id, actor), s.title, LEAST(GREATEST(1, ceil(reached * s.quorum / 100.0)::integer), 50), false, sid)
    RETURNING id INTO review;
    INSERT INTO review_subject (review_id, subject_type, user_id, group_id)
    SELECT review, p.subject_type, p.user_id, p.group_id FROM review_session_participant p WHERE p.session_id = sid AND p.role = 'approver';
    INSERT INTO review_session_item (session_id, requirement_id, round, revision, review_id) VALUES (sid, rid, s.round, r.revision, review);
    RETURN review;
END;
$$;

-- A session keeps its project, scope, moderator and quorum; its round moves on by one; once closed it changes no
-- more, but for the baseline its closing takes.
CREATE FUNCTION review_session_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.project_id, NEW.scope_kind, NEW.scope_root, NEW.scope_baseline, NEW.scope_query, NEW.since_base, NEW.since_baseline,
        NEW.moderator_id, NEW.quorum, NEW.opened_at, NEW.excluded)
       IS DISTINCT FROM (OLD.project_id, OLD.scope_kind, OLD.scope_root, OLD.scope_baseline, OLD.scope_query, OLD.since_base,
        OLD.since_baseline, OLD.moderator_id, OLD.quorum, OLD.opened_at, OLD.excluded)
       AND OLD.opened_at <> now() THEN
        RAISE EXCEPTION 'a session keeps what it was opened over' USING ERRCODE = 'check_violation', CONSTRAINT = 'review_session_fixed';
    END IF;
    IF NEW.round NOT IN (OLD.round, OLD.round + 1) THEN
        RAISE EXCEPTION 'a session moves on one round at a time' USING ERRCODE = 'check_violation', CONSTRAINT = 'review_session_round';
    END IF;
    IF OLD.state = 'closed' AND (OLD.closed_at <> now() OR NEW.state <> 'closed'
       OR (NEW.title, NEW.description, NEW.deadline, NEW.round) IS DISTINCT FROM (OLD.title, OLD.description, OLD.deadline, OLD.round)) THEN
        RAISE EXCEPTION 'a closed session does not change' USING ERRCODE = 'check_violation', CONSTRAINT = 'review_session_closed';
    END IF;
    RETURN NEW;
END;
$$;

-- Whether a baseline is the one a session's closing takes now, by its moderator.
CREATE FUNCTION review_session_closing(sid uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM review_session s
        WHERE s.id = sid AND s.state = 'closed' AND s.closed_at = now() AND s.baseline_id IS NULL
          AND review_session_moderated(sid, actor))
$$;

-- Whether the actor may decide a review: named on it, able to view the requirement, while it is pending and its
-- revision is the requirement's, not the revision's author unless the rule allowed it, and, for a session's review,
-- while its session is open.
CREATE OR REPLACE FUNCTION review_decidable(review uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM requirement_review v
        JOIN requirement r ON r.id = v.requirement_id
        JOIN requirement_revision rv ON rv.requirement_id = v.requirement_id AND rv.number = v.revision
        WHERE v.id = review AND v.state = 'pending' AND r.revision = v.revision
          AND (v.author_may_approve OR rv.created_by IS DISTINCT FROM actor)
          AND perm_requirement_viewable(v.requirement_id, actor) AND review_names(review, actor)
          AND (v.session_id IS NULL OR EXISTS (SELECT 1 FROM review_session s WHERE s.id = v.session_id AND s.state = 'open')))
$$;
-- +goose StatementEnd

CREATE TRIGGER review_session_guard BEFORE UPDATE ON review_session
    FOR EACH ROW EXECUTE FUNCTION review_session_guard();

ALTER TABLE review_session             ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_session             FORCE  ROW LEVEL SECURITY;
ALTER TABLE review_session_participant ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_session_participant FORCE  ROW LEVEL SECURITY;
ALTER TABLE review_session_round       ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_session_round       FORCE  ROW LEVEL SECURITY;
ALTER TABLE review_session_item        ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_session_item        FORCE  ROW LEVEL SECURITY;

CREATE POLICY review_session_installation ON review_session USING (true) WITH CHECK (true);
CREATE POLICY review_session_participant_installation ON review_session_participant USING (true) WITH CHECK (true);
CREATE POLICY review_session_round_installation ON review_session_round USING (true) WITH CHECK (true);
CREATE POLICY review_session_item_installation ON review_session_item USING (true) WITH CHECK (true);

-- A project's readers read its sessions; its editors open them while it is open; the moderator changes one.
CREATE POLICY review_session_readers ON review_session AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY review_session_openers ON review_session AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (moderator_id = current_actor_id() AND state = 'open' AND round = 1
                AND perm_project_holds(project_id, current_actor_id(), 'edit')
                AND EXISTS (SELECT 1 FROM project p WHERE p.id = project_id AND p.archived_at IS NULL));
CREATE POLICY review_session_moderators ON review_session AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (review_session_moderated(id, current_actor_id()))
    WITH CHECK (review_session_moderated(id, current_actor_id()));
-- What a session holds shows with it; its participants are named as it opens, its rounds by its moderator, and its
-- items only by review_session_pin.
CREATE POLICY review_session_participant_readers ON review_session_participant AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id));
CREATE POLICY review_session_participant_namers ON review_session_participant AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id AND s.opened_at = now() AND s.moderator_id = current_actor_id()));
CREATE POLICY review_session_round_readers ON review_session_round AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id));
CREATE POLICY review_session_round_starters ON review_session_round AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (started_by = current_actor_id() AND review_session_moderated(session_id, current_actor_id())
                AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id AND s.state = 'open' AND s.round = number));
CREATE POLICY review_session_item_readers ON review_session_item AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id));

-- A session's reviews are asked only by review_session_pin, which holds them to the session.
CREATE POLICY requirement_review_not_in_session ON requirement_review AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (session_id IS NULL);

-- A baseline is taken by the project's administrators, or by the moderator of a session closing now, of it.
DROP POLICY baseline_takers ON baseline;
CREATE POLICY baseline_takers ON baseline AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id()
                AND (perm_project_holds(project_id, current_actor_id(), 'administer')
                     OR (session_id IS NOT NULL AND review_session_closing(session_id, current_actor_id())))
                AND EXISTS (SELECT 1 FROM project p WHERE p.id = project_id AND p.archived_at IS NULL));

-- A notification about a session is for whoever may read its project.
DROP POLICY notification_own_read ON notification;
DROP POLICY notification_own_write ON notification;
CREATE POLICY notification_own_read ON notification AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (user_id = current_actor_id() AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id()))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))));
CREATE POLICY notification_own_write ON notification AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (user_id = current_actor_id() AND read_at IS NULL AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id()))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))));

GRANT SELECT, INSERT, UPDATE, DELETE ON review_session, review_session_participant, review_session_round, review_session_item TO exciter_admin;
REVOKE ALL ON review_session, review_session_participant, review_session_round, review_session_item FROM exciter_app;
GRANT SELECT, INSERT ON review_session, review_session_participant, review_session_round TO exciter_app;
GRANT UPDATE (title, description, deadline, reminded_at, state, round, baseline_id, closed_at) ON review_session TO exciter_app;
GRANT SELECT ON review_session_item TO exciter_app;

-- +goose Down
DROP POLICY IF EXISTS notification_own_read ON notification;
DROP POLICY IF EXISTS notification_own_write ON notification;
DELETE FROM notification WHERE requirement_id IS NULL OR kind LIKE 'session_%';
CREATE POLICY notification_own_read ON notification AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (user_id = current_actor_id() AND perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY notification_own_write ON notification AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (user_id = current_actor_id() AND read_at IS NULL
                AND perm_requirement_viewable(requirement_id, current_actor_id()));
ALTER TABLE notification DROP CONSTRAINT notification_kind_check,
    ADD CONSTRAINT notification_kind_check CHECK (kind IN ('mentioned', 'review_requested', 'reviewed', 'suspect', 'replied',
        'commented', 'resolved', 'revised', 'transitioned', 'source_revised')),
    DROP CONSTRAINT notification_about_something, DROP COLUMN session_id, ALTER COLUMN requirement_id SET NOT NULL;
DROP POLICY IF EXISTS baseline_takers ON baseline;
CREATE POLICY baseline_takers ON baseline AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND perm_project_holds(project_id, current_actor_id(), 'administer')
                AND EXISTS (SELECT 1 FROM project p WHERE p.id = project_id AND p.archived_at IS NULL));
ALTER TABLE baseline DROP COLUMN session_id;
DROP POLICY IF EXISTS requirement_review_not_in_session ON requirement_review;
DROP TABLE review_session_item;
DELETE FROM requirement_review WHERE session_id IS NOT NULL;
ALTER TABLE requirement_review DROP CONSTRAINT requirement_review_required,
    ADD CONSTRAINT requirement_review_required_check CHECK (required BETWEEN 1 AND 10);
ALTER TABLE requirement_review DROP COLUMN session_id;
DROP TABLE review_session_round;
DROP TABLE review_session_participant;
DROP TABLE review_session;
DROP FUNCTION review_session_closing(uuid, uuid);
DROP FUNCTION review_session_guard();
DROP FUNCTION review_session_pin(uuid, uuid);
DROP FUNCTION review_session_takes_part(uuid, uuid, text);
DROP FUNCTION review_session_moderated(uuid, uuid);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION review_decidable(review uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM requirement_review v
        JOIN requirement r ON r.id = v.requirement_id
        JOIN requirement_revision rv ON rv.requirement_id = v.requirement_id AND rv.number = v.revision
        WHERE v.id = review AND v.state = 'pending' AND r.revision = v.revision
          AND (v.author_may_approve OR rv.created_by IS DISTINCT FROM actor)
          AND perm_requirement_viewable(v.requirement_id, actor) AND review_names(review, actor))
$$;
-- +goose StatementEnd
