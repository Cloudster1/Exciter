-- +goose Up
-- Reuse and variants (#49): a requirement copied from a library project that follows its source. It keeps the source
-- and the source's revision it shows; while it follows, its words are the source's, which this file holds it to.

ALTER TABLE requirement
    ADD COLUMN reuse_source   uuid,
    ADD COLUMN reuse_revision integer,
    -- following: its words are the source's at reuse_revision; variant: they are its own, branched there.
    ADD COLUMN reuse_state    text CHECK (reuse_state IN ('following', 'variant')),
    ADD CONSTRAINT requirement_reuse_whole CHECK (num_nulls(reuse_source, reuse_revision, reuse_state) IN (0, 3)),
    ADD CONSTRAINT requirement_reuse_revision_positive CHECK (reuse_revision >= 1),
    -- A purged source leaves a requirement of its own, words and all; the guard clears the rest of the reuse.
    ADD CONSTRAINT requirement_reuse_source_fkey FOREIGN KEY (reuse_source) REFERENCES requirement(id) ON DELETE SET NULL;

CREATE INDEX requirement_reuse_source_idx ON requirement (reuse_source) WHERE reuse_source IS NOT NULL;

-- +goose StatementBegin
-- Words are the title and the plain text of the statement and the rationale: a copy's images have their own ids, so
-- the documents themselves differ while what they say is the source's.
CREATE FUNCTION requirement_reuse_words_match(r requirement, s requirement) RETURNS boolean
    LANGUAGE sql STABLE SET search_path = public, pg_temp
    RETURN r.kind = s.kind AND r.title = s.title
       AND requirement_plain_text(r.statement) IS NOT DISTINCT FROM requirement_plain_text(s.statement)
       AND requirement_plain_text(r.rationale) IS NOT DISTINCT FROM requirement_plain_text(s.rationale);

-- A reuse starts from a library requirement of another project the writer may read, at its revision now, with its
-- words. Afterwards it may become a variant or lose its source, but never gain another; while it follows, a change
-- of its words or its revision of the source is a take of the source as it is now.
CREATE FUNCTION requirement_reuse_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    s       requirement;
    library boolean;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.reuse_source IS DISTINCT FROM OLD.reuse_source AND NEW.reuse_source IS NOT NULL THEN
        RAISE EXCEPTION 'a requirement does not take another source' USING ERRCODE = 'check_violation',
            CONSTRAINT = 'requirement_reuse_source_fixed';
    END IF;
    IF NEW.reuse_source IS NULL THEN
        NEW.reuse_revision := NULL;
        NEW.reuse_state := NULL;
        RETURN NEW;
    END IF;
    SELECT * INTO s FROM requirement WHERE id = NEW.reuse_source;
    IF TG_OP = 'INSERT' THEN
        SELECT COALESCE((p.settings ->> 'library')::boolean, false) INTO library FROM project p WHERE p.id = s.project_id;
        IF NOT COALESCE(library, false) OR s.project_id = NEW.project_id OR s.trashed_at IS NOT NULL
           OR (current_actor_id() IS NOT NULL AND NOT perm_requirement_viewable(s.id, current_actor_id())) THEN
            RAISE EXCEPTION 'only a library requirement of another project, out of the trash, is reused'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_reuse_from_library';
        END IF;
        IF NEW.reuse_state <> 'following' THEN
            RAISE EXCEPTION 'a reuse starts by following its source' USING ERRCODE = 'check_violation',
                CONSTRAINT = 'requirement_reuse_follows_first';
        END IF;
    ELSIF NEW.reuse_state = 'variant' THEN
        IF NEW.reuse_revision IS DISTINCT FROM OLD.reuse_revision THEN
            RAISE EXCEPTION 'a variant keeps the revision it branched from' USING ERRCODE = 'check_violation',
                CONSTRAINT = 'requirement_reuse_branch_fixed';
        END IF;
        RETURN NEW;
    ELSIF OLD.reuse_state = 'following' AND NEW.reuse_revision = OLD.reuse_revision
          AND NEW.title = OLD.title AND NEW.statement IS NOT DISTINCT FROM OLD.statement
          AND NEW.rationale IS NOT DISTINCT FROM OLD.rationale THEN
        RETURN NEW;
    END IF;
    IF NEW.reuse_revision <> s.revision OR NOT requirement_reuse_words_match(NEW, s) THEN
        RAISE EXCEPTION 'a requirement that follows its source has the source''s words at its latest revision'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_reuse_follows_source';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_reuse_guard
    BEFORE INSERT OR UPDATE OF title, statement, rationale, reuse_source, reuse_revision, reuse_state ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_reuse_guard();

-- Becoming a variant, or following again, is logged beside the other changes that write no revision.
ALTER TABLE requirement_change DROP CONSTRAINT requirement_change_field_check,
    ADD CONSTRAINT requirement_change_field_check CHECK (field IN ('status', 'owner', 'priority', 'labels', 'place', 'key', 'link',
        'attachment', 'trash', 'reuse'));

-- A reuse is made by an insert and changed by the guard's rules; its source is never written again but by the key.
GRANT UPDATE (reuse_revision, reuse_state) ON requirement TO exciter_app;

-- Watchers of a reuse are told when its source has a new revision.
ALTER TABLE notification DROP CONSTRAINT notification_kind_check,
    ADD CONSTRAINT notification_kind_check CHECK (kind IN ('mentioned', 'review_requested', 'reviewed', 'suspect', 'replied',
        'commented', 'resolved', 'revised', 'transitioned', 'source_revised'));

-- +goose Down
DELETE FROM notification WHERE kind = 'source_revised';
ALTER TABLE notification DROP CONSTRAINT notification_kind_check,
    ADD CONSTRAINT notification_kind_check CHECK (kind IN ('mentioned', 'review_requested', 'reviewed', 'suspect', 'replied',
        'commented', 'resolved', 'revised', 'transitioned'));
REVOKE UPDATE (reuse_revision, reuse_state) ON requirement FROM exciter_app;
DELETE FROM requirement_change WHERE field = 'reuse';
ALTER TABLE requirement_change DROP CONSTRAINT requirement_change_field_check,
    ADD CONSTRAINT requirement_change_field_check CHECK (field IN ('status', 'owner', 'priority', 'labels', 'place', 'key', 'link',
        'attachment', 'trash'));
DROP TRIGGER requirement_reuse_guard ON requirement;
DROP FUNCTION requirement_reuse_guard();
DROP FUNCTION requirement_reuse_words_match(requirement, requirement);
ALTER TABLE requirement DROP COLUMN reuse_state, DROP COLUMN reuse_revision, DROP COLUMN reuse_source;
UPDATE project SET settings = settings - 'library';
