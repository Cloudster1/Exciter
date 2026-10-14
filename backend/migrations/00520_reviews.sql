-- +goose Up
-- Review and approval of requirement revisions (#35), shaped after Stator's 00680 page reviews for a different act:
-- an Exciter review signs off a revision that already is. A rule names the approvers of a project's requirements of
-- one type, or of every type without its own; a review pins one revision and freezes the rule's approvers and count,
-- so changing the rule moves no goal posts. The database holds what the service promises.
CREATE TABLE approval_rule (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id         uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    type_id            uuid REFERENCES requirement_type(id) ON DELETE CASCADE,
    -- review.MinRequired to review.MaxRequired.
    required           integer NOT NULL CHECK (required BETWEEN 1 AND 10),
    author_may_approve boolean NOT NULL DEFAULT false,
    updated_by         uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at         timestamptz NOT NULL DEFAULT now()
);
-- One rule per type, and one for every type without its own.
CREATE UNIQUE INDEX approval_rule_type_idx ON approval_rule (project_id, type_id) NULLS NOT DISTINCT;

-- People and groups, never everyone: an approval is somebody's judgement.
CREATE TABLE approval_rule_subject (
    rule_id      uuid NOT NULL REFERENCES approval_rule(id) ON DELETE CASCADE,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
    user_id      uuid REFERENCES app_user(id) ON DELETE CASCADE,
    group_id     uuid REFERENCES groups(id) ON DELETE CASCADE,
    CONSTRAINT approval_rule_subject_whole CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
        (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL))
);
CREATE UNIQUE INDEX approval_rule_subject_idx ON approval_rule_subject (rule_id, subject_type, user_id, group_id) NULLS NOT DISTINCT;

CREATE TABLE requirement_review (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    requirement_id     uuid NOT NULL,
    revision           integer NOT NULL,
    requested_by       uuid REFERENCES app_user(id) ON DELETE SET NULL,
    -- review.MaxCommentLength.
    comment            text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
    required           integer NOT NULL CHECK (required BETWEEN 1 AND 10),
    author_may_approve boolean NOT NULL,
    state              text NOT NULL DEFAULT 'pending'
                       CHECK (state IN ('pending', 'approved', 'rejected', 'withdrawn', 'superseded')),
    requested_at       timestamptz NOT NULL DEFAULT now(),
    finished_at        timestamptz,
    CONSTRAINT requirement_review_finished CHECK ((state = 'pending') = (finished_at IS NULL)),
    FOREIGN KEY (requirement_id, revision) REFERENCES requirement_revision (requirement_id, number) ON DELETE CASCADE
);
CREATE UNIQUE INDEX requirement_review_pending_idx ON requirement_review (requirement_id) WHERE state = 'pending';
CREATE INDEX requirement_review_requirement_idx ON requirement_review (requirement_id, requested_at DESC, id DESC);

CREATE TABLE review_subject (
    review_id    uuid NOT NULL REFERENCES requirement_review(id) ON DELETE CASCADE,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
    user_id      uuid REFERENCES app_user(id) ON DELETE CASCADE,
    group_id     uuid REFERENCES groups(id) ON DELETE CASCADE,
    CONSTRAINT review_subject_whole CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
        (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL))
);
CREATE UNIQUE INDEX review_subject_idx ON review_subject (review_id, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE INDEX review_subject_user_idx ON review_subject (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX review_subject_group_idx ON review_subject (group_id) WHERE group_id IS NOT NULL;

-- One row per approver, rewritten by their next word: the last one counts.
CREATE TABLE review_decision (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    review_id   uuid NOT NULL REFERENCES requirement_review(id) ON DELETE CASCADE,
    reviewer_id uuid REFERENCES app_user(id) ON DELETE SET NULL,
    decision    text NOT NULL CHECK (decision IN ('approve', 'reject')),
    -- review.MaxCommentLength.
    comment     text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
    decided_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (review_id, reviewer_id)
);

-- What a person was told about a review goes with it.
ALTER TABLE notification ADD CONSTRAINT notification_review_fk
    FOREIGN KEY (review_id) REFERENCES requirement_review(id) ON DELETE CASCADE;

-- +goose StatementBegin
CREATE FUNCTION approval_rule_holds(rule uuid, actor uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM approval_rule r WHERE r.id = rule AND perm_project_holds(r.project_id, actor, permission))
$$;

-- The rule a requirement is held to: its type's in its project, else the project's for every other type.
CREATE FUNCTION approval_rule_of(target uuid) RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT a.id FROM requirement r
    JOIN approval_rule a ON a.project_id = r.project_id AND (a.type_id = r.type_id OR a.type_id IS NULL)
    WHERE r.id = target
    ORDER BY a.type_id IS NULL
    LIMIT 1
$$;

-- Whether a review names the actor, directly or by a group they are in.
CREATE FUNCTION review_names(review uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM review_subject s
        WHERE s.review_id = review
          AND (s.user_id = actor OR EXISTS (SELECT 1 FROM group_member m WHERE m.group_id = s.group_id AND m.user_id = actor)))
$$;

-- Whether the actor may decide a review: named on it, able to view the requirement, while it is pending and its
-- revision is the requirement's, and not the revision's author unless the rule allowed it.
CREATE FUNCTION review_decidable(review uuid, actor uuid) RETURNS boolean
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

CREATE FUNCTION review_viewable(review uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM requirement_review v WHERE v.id = review AND perm_requirement_viewable(v.requirement_id, actor))
$$;

CREATE FUNCTION review_approvals(review uuid) RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT count(*)::integer FROM review_decision d WHERE d.review_id = review AND d.decision = 'approve'
$$;

-- Whether a review may still be given its approvers: its requester's, pending, undecided, and only those its
-- requirement's rule names.
CREATE FUNCTION review_subject_frozen(review uuid, kind text, person uuid, team uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM requirement_review v
        JOIN approval_rule_subject s ON s.rule_id = approval_rule_of(v.requirement_id)
        WHERE v.id = review AND v.requested_by = actor AND v.state = 'pending'
          AND NOT EXISTS (SELECT 1 FROM review_decision d WHERE d.review_id = v.id)
          AND s.subject_type = kind AND s.user_id IS NOT DISTINCT FROM person AND s.group_id IS NOT DISTINCT FROM team)
$$;

-- Whether a review asks what the requirement's rule says now: its count and whether the author may approve.
CREATE FUNCTION review_follows_rule(target uuid, required integer, author_may_approve boolean) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM approval_rule a
        WHERE a.id = approval_rule_of(target) AND a.required = review_follows_rule.required
          AND a.author_may_approve = review_follows_rule.author_may_approve)
$$;

-- Whether a requirement has an approved review of its current revision, which condition.approved asks.
CREATE FUNCTION requirement_approved(target uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM requirement_review v JOIN requirement r ON r.id = v.requirement_id
        WHERE v.requirement_id = target AND v.state = 'approved' AND v.revision = r.revision)
$$;

-- A review leaves pending once, by the way its state says: withdrawn by whoever asked or an administrator of the
-- project, rejected by an approver who rejected it, approved with its approvals in. Only a new revision supersedes
-- one, through requirement_review_supersede.
CREATE FUNCTION requirement_review_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'exciter_app' THEN
        RETURN NEW;
    END IF;
    IF OLD.state <> 'pending' THEN
        RAISE EXCEPTION 'a review that was closed stays as it was closed' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'pending' THEN
        RETURN NEW;
    END IF;
    IF NEW.state = 'withdrawn' AND OLD.requested_by IS DISTINCT FROM actor AND NOT EXISTS (
        SELECT 1 FROM requirement r WHERE r.id = OLD.requirement_id AND perm_project_holds(r.project_id, actor, 'administer')) THEN
        RAISE EXCEPTION 'only whoever asked, or an administrator of the project, withdraws a review' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'rejected' AND NOT EXISTS (
        SELECT 1 FROM review_decision d WHERE d.review_id = OLD.id AND d.reviewer_id = actor AND d.decision = 'reject') THEN
        RAISE EXCEPTION 'only an approver who rejected it rejects a review' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'approved' AND review_approvals(OLD.id) < OLD.required THEN
        RAISE EXCEPTION 'a review is approved only with its approvals' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'superseded' THEN
        RAISE EXCEPTION 'only a new revision supersedes a review' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;

-- A new revision supersedes the review pending on an older one, whoever wrote it; an approved review stays, of the
-- revision it named.
CREATE FUNCTION requirement_review_supersede() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    closed uuid;
BEGIN
    UPDATE requirement_review SET state = 'superseded', finished_at = now()
    WHERE requirement_id = NEW.requirement_id AND state = 'pending' AND revision < NEW.number
    RETURNING id INTO closed;
    IF closed IS NOT NULL THEN
        INSERT INTO outbox_event (topic, payload)
        VALUES ('review.closed', jsonb_build_object('reviewId', closed, 'requirementId', NEW.requirement_id,
                                                   'state', 'superseded', 'actorId', NEW.created_by));
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_review_guard BEFORE UPDATE ON requirement_review
    FOR EACH ROW EXECUTE FUNCTION requirement_review_guard();
CREATE TRIGGER requirement_revision_supersedes AFTER INSERT ON requirement_revision
    FOR EACH ROW EXECUTE FUNCTION requirement_review_supersede();

-- condition.approved: a transition that asks it goes through only with the current revision approved.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_transition(requirement uuid, transition uuid, note text DEFAULT ''::text)
 RETURNS uuid
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    r requirement%ROWTYPE;
    t workflow_transition%ROWTYPE;
    actor uuid := current_actor_id();
    new_owner uuid;
    post text;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_transition.requirement FOR UPDATE;
    SELECT * INTO t FROM workflow_transition WHERE id = requirement_transition.transition;
    IF r.id IS NULL OR t.id IS NULL OR r.trashed_at IS NOT NULL OR t.workflow_id <> requirement_workflow(r.type_id, r.project_id) THEN
        RAISE EXCEPTION 'that transition is not one of the requirement''s workflow'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_transition_known';
    END IF;
    IF NOT (perm_project_holds(r.project_id, actor, 'edit') OR (perm_project_holds(r.project_id, actor, 'approve') AND EXISTS (
        SELECT 1 FROM transition_rule x WHERE x.transition_id = t.id AND x.type = 'condition.project_role' AND x.config -> 'roles' ? 'reviewer'))) THEN
        RAISE EXCEPTION 'the transition is not the actor''s to take'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'requirement_transition_allowed';
    END IF;
    IF EXISTS (SELECT 1 FROM transition_rule x WHERE x.transition_id = t.id AND x.type = 'condition.approved')
       AND NOT requirement_approved(r.id) THEN
        RAISE EXCEPTION 'the transition asks for the current revision to be approved'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_transition_approved';
    END IF;
    IF (t.from_status_id IS NOT NULL AND t.from_status_id IS DISTINCT FROM r.status_id)
       OR (t.from_status_id IS NULL AND t.to_status_id = r.status_id) THEN
        RAISE EXCEPTION 'the transition does not leave the requirement''s status'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_transition_leaves';
    END IF;
    new_owner := r.owner_id;
    FOR post IN SELECT x.type FROM transition_rule x WHERE x.transition_id = t.id AND x.kind = 'postfunction' ORDER BY x.position LOOP
        new_owner := CASE post WHEN 'postfunction.assign_to_actor' THEN actor WHEN 'postfunction.clear_owner' THEN NULL ELSE new_owner END;
    END LOOP;
    UPDATE requirement SET status_id = t.to_status_id, owner_id = new_owner, updated_by = actor WHERE id = r.id;
    INSERT INTO requirement_change (requirement_id, field, from_value, to_value, reason, actor_id)
    VALUES (r.id, 'status', COALESCE((SELECT name FROM req_status WHERE id = r.status_id), ''),
            (SELECT name FROM req_status WHERE id = t.to_status_id), COALESCE(note, ''), actor);
    IF new_owner IS DISTINCT FROM r.owner_id THEN
        INSERT INTO requirement_change (requirement_id, field, from_value, to_value, actor_id)
        VALUES (r.id, 'owner', COALESCE((SELECT name FROM app_user WHERE id = r.owner_id), ''),
                COALESCE((SELECT name FROM app_user WHERE id = new_owner), ''), actor);
    END IF;
    RETURN t.to_status_id;
END;
$function$;
-- +goose StatementEnd

ALTER TABLE approval_rule         ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_rule         FORCE  ROW LEVEL SECURITY;
ALTER TABLE approval_rule_subject ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_rule_subject FORCE  ROW LEVEL SECURITY;
ALTER TABLE requirement_review    ENABLE ROW LEVEL SECURITY;
ALTER TABLE requirement_review    FORCE  ROW LEVEL SECURITY;
ALTER TABLE review_subject        ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_subject        FORCE  ROW LEVEL SECURITY;
ALTER TABLE review_decision       ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_decision       FORCE  ROW LEVEL SECURITY;

-- One open policy per table, as every table has since #106, and the permission policies beside it.
CREATE POLICY approval_rule_installation ON approval_rule USING (true) WITH CHECK (true);
CREATE POLICY approval_rule_subject_installation ON approval_rule_subject USING (true) WITH CHECK (true);
CREATE POLICY requirement_review_installation ON requirement_review USING (true) WITH CHECK (true);
CREATE POLICY review_subject_installation ON review_subject USING (true) WITH CHECK (true);
CREATE POLICY review_decision_installation ON review_decision USING (true) WITH CHECK (true);

-- A project's rules are read by whoever views it and set by whoever administers it.
CREATE POLICY approval_rule_readers ON approval_rule AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY approval_rule_setters ON approval_rule AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY approval_rule_changers ON approval_rule AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer'))
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY approval_rule_removers ON approval_rule AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY approval_rule_subject_readers ON approval_rule_subject AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (approval_rule_holds(rule_id, current_actor_id(), 'view'));
CREATE POLICY approval_rule_subject_setters ON approval_rule_subject AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (approval_rule_holds(rule_id, current_actor_id(), 'administer'));
CREATE POLICY approval_rule_subject_removers ON approval_rule_subject AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (approval_rule_holds(rule_id, current_actor_id(), 'administer'));

-- A review is read with its requirement. It is asked for by an editor of a requirement, never a section, of its
-- current revision, pending, as its rule says now.
CREATE POLICY requirement_review_readers ON requirement_review AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY requirement_review_askers ON requirement_review AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (requested_by = current_actor_id() AND perm_requirement_editable(requirement_id, current_actor_id())
                AND state = 'pending' AND finished_at IS NULL
                AND EXISTS (SELECT 1 FROM requirement r WHERE r.id = requirement_id AND r.kind = 'requirement'
                              AND r.trashed_at IS NULL AND r.revision = requirement_review.revision)
                AND review_follows_rule(requirement_id, required, author_may_approve));
-- How it may move on is requirement_review_guard's.
CREATE POLICY requirement_review_closers ON requirement_review AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()))
    WITH CHECK (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY review_subject_readers ON review_subject AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (review_viewable(review_id, current_actor_id()));
CREATE POLICY review_subject_freezers ON review_subject AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (review_subject_frozen(review_id, subject_type, user_id, group_id, current_actor_id()));
CREATE POLICY review_decision_readers ON review_decision AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (review_viewable(review_id, current_actor_id()));
CREATE POLICY review_decision_makers ON review_decision AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (reviewer_id = current_actor_id() AND review_decidable(review_id, current_actor_id()));
CREATE POLICY review_decision_changers ON review_decision AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (reviewer_id = current_actor_id() AND review_decidable(review_id, current_actor_id()))
    WITH CHECK (reviewer_id = current_actor_id() AND review_decidable(review_id, current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON approval_rule, approval_rule_subject, requirement_review, review_subject,
    review_decision TO exciter_admin;
REVOKE ALL ON approval_rule, approval_rule_subject, requirement_review, review_subject, review_decision FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON approval_rule, approval_rule_subject TO exciter_app;
GRANT UPDATE (required, author_may_approve, updated_by, updated_at) ON approval_rule TO exciter_app;
GRANT SELECT, INSERT ON requirement_review, review_subject, review_decision TO exciter_app;
GRANT UPDATE (state, finished_at) ON requirement_review TO exciter_app;
GRANT UPDATE (decision, comment, decided_at) ON review_decision TO exciter_app;

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.requirement_transition(requirement uuid, transition uuid, note text DEFAULT ''::text)
 RETURNS uuid
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    r requirement%ROWTYPE;
    t workflow_transition%ROWTYPE;
    actor uuid := current_actor_id();
    new_owner uuid;
    post text;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_transition.requirement FOR UPDATE;
    SELECT * INTO t FROM workflow_transition WHERE id = requirement_transition.transition;
    IF r.id IS NULL OR t.id IS NULL OR r.trashed_at IS NOT NULL OR t.workflow_id <> requirement_workflow(r.type_id, r.project_id) THEN
        RAISE EXCEPTION 'that transition is not one of the requirement''s workflow'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_transition_known';
    END IF;
    IF NOT (perm_project_holds(r.project_id, actor, 'edit') OR (perm_project_holds(r.project_id, actor, 'approve') AND EXISTS (
        SELECT 1 FROM transition_rule x WHERE x.transition_id = t.id AND x.type = 'condition.project_role' AND x.config -> 'roles' ? 'reviewer'))) THEN
        RAISE EXCEPTION 'the transition is not the actor''s to take'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'requirement_transition_allowed';
    END IF;
    IF (t.from_status_id IS NOT NULL AND t.from_status_id IS DISTINCT FROM r.status_id)
       OR (t.from_status_id IS NULL AND t.to_status_id = r.status_id) THEN
        RAISE EXCEPTION 'the transition does not leave the requirement''s status'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_transition_leaves';
    END IF;
    new_owner := r.owner_id;
    FOR post IN SELECT x.type FROM transition_rule x WHERE x.transition_id = t.id AND x.kind = 'postfunction' ORDER BY x.position LOOP
        new_owner := CASE post WHEN 'postfunction.assign_to_actor' THEN actor WHEN 'postfunction.clear_owner' THEN NULL ELSE new_owner END;
    END LOOP;
    UPDATE requirement SET status_id = t.to_status_id, owner_id = new_owner, updated_by = actor WHERE id = r.id;
    INSERT INTO requirement_change (requirement_id, field, from_value, to_value, reason, actor_id)
    VALUES (r.id, 'status', COALESCE((SELECT name FROM req_status WHERE id = r.status_id), ''),
            (SELECT name FROM req_status WHERE id = t.to_status_id), COALESCE(note, ''), actor);
    IF new_owner IS DISTINCT FROM r.owner_id THEN
        INSERT INTO requirement_change (requirement_id, field, from_value, to_value, actor_id)
        VALUES (r.id, 'owner', COALESCE((SELECT name FROM app_user WHERE id = r.owner_id), ''),
                COALESCE((SELECT name FROM app_user WHERE id = new_owner), ''), actor);
    END IF;
    RETURN t.to_status_id;
END;
$function$;
-- +goose StatementEnd
DROP TRIGGER requirement_revision_supersedes ON requirement_revision;
ALTER TABLE notification DROP CONSTRAINT notification_review_fk;
DROP TABLE review_decision;
DROP TABLE review_subject;
DROP TABLE requirement_review;
DROP TABLE approval_rule_subject;
DROP TABLE approval_rule;
DROP FUNCTION requirement_review_supersede();
DROP FUNCTION requirement_review_guard();
DROP FUNCTION requirement_approved(uuid);
DROP FUNCTION review_follows_rule(uuid, integer, boolean);
DROP FUNCTION review_subject_frozen(uuid, text, uuid, uuid, uuid);
DROP FUNCTION review_approvals(uuid);
DROP FUNCTION review_viewable(uuid, uuid);
DROP FUNCTION review_decidable(uuid, uuid);
DROP FUNCTION review_names(uuid, uuid);
DROP FUNCTION approval_rule_of(uuid);
DROP FUNCTION approval_rule_holds(uuid, uuid, text);
