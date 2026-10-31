-- +goose Up
-- A session whose approvers reach nobody pins reviews that can never be decided (#463). The service now refuses it,
-- and the pin refuses it too, so a writer straight through SQL cannot leave one either.
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
