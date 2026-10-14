-- +goose Up
-- Suspect links (#36). A trace link remembers the revision of each end it was made or last confirmed against, and
-- is suspect while an end its type watches has a newer one; that is computed here, never stored. Clearing confirms
-- a link against both ends as the clearer read them, and is recorded for good. An Armature link remembers its
-- requirement's revision the same way.

-- Every link already made takes its requirement's revision as confirmed, so nothing turns suspect by upgrading.
ALTER TABLE armature_issue_link ADD COLUMN reviewed_rev integer;
UPDATE armature_issue_link l SET reviewed_rev = r.revision FROM requirement r WHERE r.id = l.requirement_id;
ALTER TABLE armature_issue_link ALTER COLUMN reviewed_rev SET NOT NULL;

-- +goose StatementBegin
-- A new Armature link is confirmed against the requirement as it is, whatever the writer says.
CREATE FUNCTION armature_issue_link_reviewed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.reviewed_rev := (SELECT revision FROM requirement WHERE id = NEW.requirement_id);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER armature_issue_link_reviewed BEFORE INSERT ON armature_issue_link
    FOR EACH ROW EXECUTE FUNCTION armature_issue_link_reviewed();

-- Each live link with its ends' revisions now and as confirmed. An end moved when its type watches it and it has a
-- newer revision; a link with an end in the trash is not suspect until the end comes back. Suspect since is when
-- the first revision past the confirmed one was saved. It reads as the reader, so it holds only the links whose
-- ends they view.
CREATE VIEW trace_link_state WITH (security_invoker = true) AS
SELECT l.id, l.type_id, l.source_id, l.target_id,
       l.source_reviewed_rev, s.revision AS source_revision,
       l.target_reviewed_rev, t.revision AS target_revision,
       m.source_moved, m.target_moved,
       m.source_moved OR m.target_moved AS suspect,
       LEAST(
           CASE WHEN m.source_moved THEN (SELECT v.created_at FROM requirement_revision v
                                          WHERE v.requirement_id = l.source_id AND v.number = l.source_reviewed_rev + 1) END,
           CASE WHEN m.target_moved THEN (SELECT v.created_at FROM requirement_revision v
                                          WHERE v.requirement_id = l.target_id AND v.number = l.target_reviewed_rev + 1) END
       ) AS suspect_since
FROM trace_link l
JOIN link_type lt ON lt.id = l.type_id
JOIN requirement s ON s.id = l.source_id
JOIN requirement t ON t.id = l.target_id
CROSS JOIN LATERAL (
    SELECT s.trashed_at IS NULL AND t.trashed_at IS NULL AND lt.suspect_end IN ('source', 'both')
               AND s.revision > l.source_reviewed_rev AS source_moved,
           s.trashed_at IS NULL AND t.trashed_at IS NULL AND lt.suspect_end IN ('target', 'both')
               AND t.revision > l.target_reviewed_rev AS target_moved
) m
WHERE l.deleted_at IS NULL;

-- Who confirmed a link, against which revisions and why: a trace link's two ends, or an Armature link's
-- requirement alone. Only the clearing functions write it, and nothing changes it after.
CREATE TABLE trace_link_clearance (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    link_id          uuid REFERENCES trace_link(id) ON DELETE CASCADE,
    armature_link_id uuid REFERENCES armature_issue_link(id) ON DELETE CASCADE,
    -- An Armature link's requirement is its source.
    source_rev       integer NOT NULL CHECK (source_rev > 0),
    target_rev       integer CHECK (target_rev > 0),
    comment          text NOT NULL,
    cleared_by       uuid REFERENCES app_user(id) ON DELETE SET NULL,
    cleared_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT trace_link_clearance_of_one CHECK (
        (link_id IS NULL) <> (armature_link_id IS NULL) AND (link_id IS NULL) = (target_rev IS NULL)),
    CONSTRAINT trace_link_clearance_comment CHECK (btrim(comment) <> '' AND char_length(comment) <= 500)
);

CREATE INDEX trace_link_clearance_link_idx ON trace_link_clearance (link_id, cleared_at DESC) WHERE link_id IS NOT NULL;
CREATE INDEX trace_link_clearance_armature_idx ON trace_link_clearance (armature_link_id) WHERE armature_link_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION trace_link_clearance_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'a clearance stays as it was recorded'
        USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_clearance_fixed';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER trace_link_clearance_fixed BEFORE UPDATE ON trace_link_clearance
    FOR EACH ROW EXECUTE FUNCTION trace_link_clearance_fixed();

ALTER TABLE trace_link_clearance ENABLE ROW LEVEL SECURITY;
ALTER TABLE trace_link_clearance FORCE  ROW LEVEL SECURITY;
CREATE POLICY trace_link_clearance_installation ON trace_link_clearance USING (true) WITH CHECK (true);

-- A clearance shows to whoever sees its link; the subqueries read as the reader, through the links' own policies.
CREATE POLICY trace_link_clearance_readers ON trace_link_clearance AS RESTRICTIVE FOR SELECT TO exciter_app
    USING ((link_id IS NOT NULL AND EXISTS (SELECT 1 FROM trace_link l WHERE l.id = link_id))
           OR (armature_link_id IS NOT NULL AND EXISTS (SELECT 1 FROM armature_issue_link a WHERE a.id = armature_link_id)));

GRANT SELECT, INSERT, DELETE ON trace_link_clearance TO exciter_admin;
REVOKE ALL ON trace_link_clearance FROM exciter_app;
GRANT SELECT ON trace_link_clearance TO exciter_app;
REVOKE ALL ON trace_link_state FROM exciter_app;
GRANT SELECT ON trace_link_state TO exciter_app, exciter_admin;
GRANT SELECT (reviewed_rev) ON armature_issue_link TO exciter_app;

-- What a person was told about a link goes with it.
ALTER TABLE notification ADD CONSTRAINT notification_link_fk
    FOREIGN KEY (link_id) REFERENCES trace_link(id) ON DELETE CASCADE;

-- +goose StatementBegin
-- Clearing a trace link as the person the transaction acts for: they see both ends and may edit one, both are out
-- of the trash, and the revisions named are the ends' current ones, so nobody confirms what they did not read. A
-- link that is gone, or one the person does not see, is no_data_found; a refusal is insufficient_privilege.
CREATE FUNCTION trace_link_clear(link uuid, named_source integer, named_target integer, why text) RETURNS uuid
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor   uuid := current_actor_id();
    l       trace_link;
    s       requirement;
    t       requirement;
    cleared uuid;
BEGIN
    SELECT * INTO l FROM trace_link WHERE id = link AND deleted_at IS NULL FOR UPDATE;
    IF NOT FOUND OR actor IS NULL
       OR NOT (perm_requirement_viewable(l.source_id, actor) AND perm_requirement_viewable(l.target_id, actor)) THEN
        RAISE EXCEPTION 'there is no such link' USING ERRCODE = 'no_data_found';
    END IF;
    IF NOT (perm_requirement_editable(l.source_id, actor) OR perm_requirement_editable(l.target_id, actor)) THEN
        RAISE EXCEPTION 'only whoever may edit an end clears a link' USING ERRCODE = 'insufficient_privilege';
    END IF;
    -- Locked, so a revision saved meanwhile waits until the clearance is in, and then turns the link again.
    SELECT * INTO s FROM requirement WHERE id = l.source_id FOR SHARE;
    SELECT * INTO t FROM requirement WHERE id = l.target_id FOR SHARE;
    IF s.trashed_at IS NOT NULL OR t.trashed_at IS NOT NULL THEN
        RAISE EXCEPTION 'a link with an end in the trash is not cleared'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_clear_trashed';
    END IF;
    IF s.revision <> named_source OR t.revision <> named_target THEN
        RAISE EXCEPTION 'a link is cleared against its ends'' current revisions'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_clear_current';
    END IF;
    INSERT INTO trace_link_clearance (link_id, source_rev, target_rev, comment, cleared_by)
    VALUES (l.id, named_source, named_target, btrim(why), actor)
    RETURNING id INTO cleared;
    UPDATE trace_link SET source_reviewed_rev = named_source, target_reviewed_rev = named_target WHERE id = l.id;
    RETURN cleared;
END;
$$;

-- Clearing an Armature link, as above against its requirement alone, which the person may edit.
CREATE FUNCTION armature_issue_link_clear(link uuid, named integer, why text) RETURNS uuid
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor   uuid := current_actor_id();
    l       armature_issue_link;
    r       requirement;
    cleared uuid;
BEGIN
    SELECT * INTO l FROM armature_issue_link WHERE id = link FOR UPDATE;
    IF NOT FOUND OR actor IS NULL OR NOT perm_requirement_viewable(l.requirement_id, actor) THEN
        RAISE EXCEPTION 'there is no such link' USING ERRCODE = 'no_data_found';
    END IF;
    IF NOT perm_requirement_editable(l.requirement_id, actor) THEN
        RAISE EXCEPTION 'only whoever may edit the requirement clears its link' USING ERRCODE = 'insufficient_privilege';
    END IF;
    SELECT * INTO r FROM requirement WHERE id = l.requirement_id FOR SHARE;
    IF r.trashed_at IS NOT NULL THEN
        RAISE EXCEPTION 'a link of a requirement in the trash is not cleared'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_clear_trashed';
    END IF;
    IF r.revision <> named THEN
        RAISE EXCEPTION 'a link is cleared against its requirement''s current revision'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_clear_current';
    END IF;
    INSERT INTO trace_link_clearance (armature_link_id, source_rev, comment, cleared_by)
    VALUES (l.id, named, btrim(why), actor)
    RETURNING id INTO cleared;
    UPDATE armature_issue_link SET reviewed_rev = named WHERE id = l.id;
    RETURN cleared;
END;
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION trace_link_clear(uuid, integer, integer, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION armature_issue_link_clear(uuid, integer, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION trace_link_clear(uuid, integer, integer, text) TO exciter_app;
GRANT EXECUTE ON FUNCTION armature_issue_link_clear(uuid, integer, text) TO exciter_app;

-- +goose Down
DROP FUNCTION armature_issue_link_clear(uuid, integer, text);
DROP FUNCTION trace_link_clear(uuid, integer, integer, text);
ALTER TABLE notification DROP CONSTRAINT notification_link_fk;
DROP TABLE trace_link_clearance;
DROP FUNCTION trace_link_clearance_fixed();
DROP VIEW trace_link_state;
DROP TRIGGER armature_issue_link_reviewed ON armature_issue_link;
DROP FUNCTION armature_issue_link_reviewed();
ALTER TABLE armature_issue_link DROP COLUMN reviewed_rev;
