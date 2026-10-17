-- +goose Up
-- Locked requirements (#205), as docs/api-contract-m8.md agrees: a status can lock the requirements in it, and a
-- project's administrators lock one by hand, with a reason. A locked requirement takes no new revision, move, trash
-- or link it is the source of, from any writer; comments, reviews and transitions stay open.

ALTER TABLE req_status ADD COLUMN locks boolean NOT NULL DEFAULT false;

-- The hand lock; the app role holds no grant on it, so only requirement_lock and requirement_unlock write it.
ALTER TABLE requirement
    ADD COLUMN locked_at   timestamptz,
    ADD COLUMN locked_by   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    ADD COLUMN lock_reason text NOT NULL DEFAULT '',
    -- requirement.MaxLockReasonLength.
    ADD CONSTRAINT requirement_lock_whole CHECK (
        (locked_at IS NULL AND locked_by IS NULL AND lock_reason = '') OR
        (locked_at IS NOT NULL AND btrim(lock_reason) <> '' AND char_length(lock_reason) <= 500));

ALTER TABLE requirement_change DROP CONSTRAINT requirement_change_field_check,
    ADD CONSTRAINT requirement_change_field_check CHECK (field IN ('status', 'owner', 'priority', 'labels', 'place', 'key', 'link',
        'attachment', 'trash', 'reuse', 'lock'));

-- +goose StatementBegin
-- Why a requirement is locked, as the sentence a refusal says, or null while it is not; a hand lock is told over a
-- status one, as the requirement answers it.
CREATE FUNCTION requirement_lock_sentence(rid uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE
        WHEN r.locked_at IS NOT NULL THEN format('%s-R%s is locked by %s: "%s". Ask a project administrator to unlock it.',
            p.key, r.number, COALESCE(u.name, 'an administrator'), r.lock_reason)
        WHEN s.locks THEN format('%s-R%s is locked in the status %s. Take it out of %s to change it.',
            p.key, r.number, s.name, s.name)
    END
    FROM requirement r
    JOIN project p ON p.id = r.project_id
    LEFT JOIN req_status s ON s.id = r.status_id
    LEFT JOIN app_user u ON u.id = r.locked_by
    WHERE r.id = rid
$$;

CREATE FUNCTION requirement_locked(rid uuid) RETURNS boolean
    LANGUAGE sql STABLE AS $$ SELECT requirement_lock_sentence(rid) IS NOT NULL $$;

-- A revision after the first is refused while the requirement is locked; the first is its making.
CREATE FUNCTION requirement_revision_unlocked() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    why text;
BEGIN
    IF NEW.number > 1 THEN
        why := requirement_lock_sentence(NEW.requirement_id);
        IF why IS NOT NULL THEN
            RAISE EXCEPTION '%', why USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_locked';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

-- What a revision writes, the place in the tree, the project and the trash stay as they are while it is locked. A
-- trigger on the columns fires for any that is set, so only a change is refused.
CREATE FUNCTION requirement_row_unlocked() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    why text;
BEGIN
    IF (NEW.title, NEW.statement, NEW.rationale, NEW.type_id, NEW.attributes, NEW.parent_id, NEW.rank, NEW.trashed_at, NEW.project_id)
       IS DISTINCT FROM (OLD.title, OLD.statement, OLD.rationale, OLD.type_id, OLD.attributes, OLD.parent_id, OLD.rank, OLD.trashed_at, OLD.project_id) THEN
        why := requirement_lock_sentence(OLD.id);
        IF why IS NOT NULL THEN
            RAISE EXCEPTION '%', why USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_locked';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

-- A link is the change of its source's editors, so a locked source neither gains nor loses one; a link to a locked
-- requirement is its source's change.
CREATE FUNCTION trace_link_unlocked() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    why text;
BEGIN
    IF TG_OP = 'INSERT' OR NEW.deleted_at IS DISTINCT FROM OLD.deleted_at THEN
        why := requirement_lock_sentence(NEW.source_id);
        IF why IS NOT NULL THEN
            RAISE EXCEPTION '%', why USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_locked';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

-- An administrator of an open project locks a requirement by hand, with a reason, or changes the reason; the change
-- log keeps both.
CREATE FUNCTION requirement_lock(rid uuid, why text) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    r requirement%ROWTYPE;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = rid FOR UPDATE;
    IF r.id IS NULL OR NOT perm_project_holds(r.project_id, actor, 'administer')
       OR EXISTS (SELECT 1 FROM project p WHERE p.id = r.project_id AND p.archived_at IS NOT NULL) THEN
        RAISE EXCEPTION 'only an administrator of an open project locks its requirements'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'requirement_lock_forbidden';
    END IF;
    IF r.kind = 'section' OR r.trashed_at IS NOT NULL THEN
        RAISE EXCEPTION 'a section or a requirement in the trash is not locked'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_lock_section';
    END IF;
    why := btrim(COALESCE(why, ''));
    UPDATE requirement SET locked_at = COALESCE(locked_at, now()), locked_by = actor, lock_reason = why WHERE id = rid;
    INSERT INTO requirement_change (requirement_id, field, from_value, to_value, reason, actor_id)
    VALUES (rid, 'lock', CASE WHEN r.locked_at IS NULL THEN '' ELSE 'locked' END, 'locked', why, actor);
END;
$$;

-- An administrator clears a hand lock; false when there was none. A lock by the status stays.
CREATE FUNCTION requirement_unlock(rid uuid) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    r requirement%ROWTYPE;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = rid FOR UPDATE;
    IF r.id IS NULL OR NOT perm_project_holds(r.project_id, actor, 'administer')
       OR EXISTS (SELECT 1 FROM project p WHERE p.id = r.project_id AND p.archived_at IS NOT NULL) THEN
        RAISE EXCEPTION 'only an administrator of an open project unlocks its requirements'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'requirement_lock_forbidden';
    END IF;
    IF r.locked_at IS NULL THEN
        RETURN false;
    END IF;
    UPDATE requirement SET locked_at = NULL, locked_by = NULL, lock_reason = '' WHERE id = rid;
    INSERT INTO requirement_change (requirement_id, field, from_value, to_value, reason, actor_id)
    VALUES (rid, 'lock', 'locked', '', r.lock_reason, actor);
    RETURN true;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER requirement_revision_unlocked BEFORE INSERT ON requirement_revision
    FOR EACH ROW EXECUTE FUNCTION requirement_revision_unlocked();
CREATE TRIGGER requirement_row_unlocked
    BEFORE UPDATE OF title, statement, rationale, type_id, attributes, parent_id, rank, trashed_at, project_id ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_row_unlocked();
CREATE TRIGGER trace_link_unlocked BEFORE INSERT OR UPDATE OF deleted_at ON trace_link
    FOR EACH ROW EXECUTE FUNCTION trace_link_unlocked();

-- +goose Down
DROP TRIGGER IF EXISTS trace_link_unlocked ON trace_link;
DROP TRIGGER IF EXISTS requirement_row_unlocked ON requirement;
DROP TRIGGER IF EXISTS requirement_revision_unlocked ON requirement_revision;
DROP FUNCTION IF EXISTS requirement_unlock(uuid);
DROP FUNCTION IF EXISTS requirement_lock(uuid, text);
DROP FUNCTION IF EXISTS trace_link_unlocked();
DROP FUNCTION IF EXISTS requirement_row_unlocked();
DROP FUNCTION IF EXISTS requirement_revision_unlocked();
DROP FUNCTION IF EXISTS requirement_locked(uuid);
DROP FUNCTION IF EXISTS requirement_lock_sentence(uuid);
DELETE FROM requirement_change WHERE field = 'lock';
ALTER TABLE requirement_change DROP CONSTRAINT requirement_change_field_check,
    ADD CONSTRAINT requirement_change_field_check CHECK (field IN ('status', 'owner', 'priority', 'labels', 'place', 'key', 'link',
        'attachment', 'trash', 'reuse'));
ALTER TABLE requirement DROP CONSTRAINT IF EXISTS requirement_lock_whole,
    DROP COLUMN IF EXISTS lock_reason, DROP COLUMN IF EXISTS locked_by, DROP COLUMN IF EXISTS locked_at;
ALTER TABLE req_status DROP COLUMN IF EXISTS locks;
