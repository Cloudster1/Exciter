-- +goose Up
-- A session whose approvers reach nobody pins reviews that can never be decided (#463). The service now refuses it,
-- and the pin refuses it too, so a writer straight through SQL cannot leave one either.

-- The people approvers reach who could decide a review, as review_decidable asks: active, with a role and the approve
-- right, and not the author. The service counts with it too, so its refusals and the pin's always agree.
-- +goose StatementBegin
CREATE FUNCTION review_approvers_reached(project uuid, users uuid[], groups uuid[], author uuid) RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT count(*)::integer FROM (
        SELECT unnest(users) AS person UNION SELECT m.user_id FROM group_member m WHERE m.group_id = ANY (groups)
    ) people
    JOIN app_user u ON u.id = people.person
    WHERE u.role IS NOT NULL AND u.is_active AND people.person IS DISTINCT FROM author
      AND perm_project_holds(project, people.person, 'approve')
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION review_session_pin(sid uuid, rid uuid) RETURNS uuid
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    s review_session%ROWTYPE;
    r requirement%ROWTYPE;
    author uuid;
    users uuid[];
    groups uuid[];
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
    SELECT array_agg(p.user_id) FILTER (WHERE p.user_id IS NOT NULL), array_agg(p.group_id) FILTER (WHERE p.group_id IS NOT NULL)
    INTO users, groups FROM review_session_participant p WHERE p.session_id = sid AND p.role = 'approver';
    reached := review_approvers_reached(s.project_id, users, groups, author);
    -- The service tells the moderator which of the two it is, since what to do about each differs.
    IF reached = 0 AND review_approvers_reached(s.project_id, users, groups, NULL) > 0 THEN
        RAISE EXCEPTION 'the session''s approvers reach nobody but the author who may approve this requirement'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'review_session_approvers_author';
    END IF;
    IF reached = 0 THEN
        RAISE EXCEPTION 'the session''s approvers reach nobody who may approve this requirement'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'review_session_approvers';
    END IF;
    INSERT INTO requirement_review (requirement_id, revision, requested_by, comment, required, author_may_approve, session_id)
    VALUES (rid, r.revision, COALESCE(s.moderator_id, actor), s.title, LEAST(GREATEST(1, ceil(reached * s.quorum / 100.0)::integer), 50), false, sid)
    RETURNING id INTO review;
    INSERT INTO review_subject (review_id, subject_type, user_id, group_id)
    SELECT review, p.subject_type, p.user_id, p.group_id FROM review_session_participant p WHERE p.session_id = sid AND p.role = 'approver';
    INSERT INTO review_session_item (session_id, requirement_id, round, revision, review_id) VALUES (sid, rid, s.round, r.revision, review);
    RETURN review;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION review_session_pin(sid uuid, rid uuid) RETURNS uuid
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
-- +goose StatementEnd

DROP FUNCTION review_approvers_reached(uuid, uuid[], uuid[], uuid);
