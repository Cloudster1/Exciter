-- +goose Up
-- The discussion on a requirement (#33), Stator's 00140, 00154 and 00155 over requirements. A thread is a first
-- comment and its replies, one level deep; its id is its first comment's id, so a link to either finds it.
CREATE TABLE comment_thread (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    requirement_id uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    created_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    resolved_at    timestamptz,
    resolved_by    uuid REFERENCES app_user(id) ON DELETE SET NULL,
    -- The comments name their requirement too, and must name the thread's.
    UNIQUE (id, requirement_id),
    -- Who resolved goes with the resolution; a resolver whose account went leaves the time alone.
    CONSTRAINT comment_thread_resolver_resolved CHECK (resolved_by IS NULL OR resolved_at IS NOT NULL)
);

CREATE INDEX comment_thread_requirement_idx ON comment_thread (requirement_id, created_at, id);

-- A deleted comment keeps its row, its author and its place in the thread as a placeholder; its words go in the
-- statement that deletes it, for good. Revision is the requirement's when the comment was written.
CREATE TABLE comment (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    thread_id      uuid NOT NULL,
    requirement_id uuid NOT NULL,
    revision       integer NOT NULL,
    author_id      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    body           jsonb,
    created_at     timestamptz NOT NULL DEFAULT now(),
    edited_at      timestamptz,
    deleted_at     timestamptz,
    deleted_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    CONSTRAINT comment_words_until_deleted CHECK ((deleted_at IS NULL) = (body IS NOT NULL)),
    CONSTRAINT comment_body_document CHECK (body IS NULL OR jsonb_typeof(body) = 'object'),
    FOREIGN KEY (thread_id, requirement_id) REFERENCES comment_thread (id, requirement_id) ON DELETE CASCADE,
    FOREIGN KEY (requirement_id, revision) REFERENCES requirement_revision (requirement_id, number) ON DELETE CASCADE
);

CREATE INDEX comment_thread_idx ON comment (thread_id, created_at, id);
CREATE INDEX comment_requirement_idx ON comment (requirement_id) WHERE deleted_at IS NULL;

-- +goose StatementBegin
-- Commenting on a requirement: viewing it, the project's comment, which an archived project holds nobody, and a
-- requirement out of the trash. A comment is not an edit, so edit does not come into it.
CREATE FUNCTION perm_requirement_commentable(target uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_requirement_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM requirement r
        WHERE r.id = target AND r.trashed_at IS NULL AND perm_project_holds(r.project_id, actor, 'comment'))
$$;

-- Deleting somebody else's comment is moderation, the project's administer; one's own is anybody's who may still
-- view the requirement.
CREATE FUNCTION perm_comment_deletable(target uuid, author uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_requirement_viewable(target, actor) AND (
        author = actor OR EXISTS (
            SELECT 1 FROM requirement r
            WHERE r.id = target AND perm_project_holds(r.project_id, actor, 'administer')))
$$;

-- A comment is about the revision its requirement has as it is written, whatever the writer says, read under the
-- requirement's lock so a revision saved meanwhile is either before it or after.
CREATE FUNCTION comment_on_current_revision() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    SELECT revision INTO NEW.revision FROM requirement WHERE id = NEW.requirement_id FOR SHARE;
    RETURN NEW;
END;
$$;

-- A policy cannot tell an edit from a delete, so this does: the body is the author's alone to change while they
-- may comment, a delete takes the words with it and names who deleted, and nothing else about a comment moves.
CREATE FUNCTION comment_write_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'exciter_app' THEN
        RETURN NEW;
    END IF;
    IF (NEW.thread_id, NEW.requirement_id, NEW.revision, NEW.author_id, NEW.created_at)
       IS DISTINCT FROM (OLD.thread_id, OLD.requirement_id, OLD.revision, OLD.author_id, OLD.created_at) THEN
        RAISE EXCEPTION 'a comment stays where and whose it is' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF OLD.deleted_at IS NOT NULL THEN
        RAISE EXCEPTION 'a deleted comment cannot change' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.deleted_at IS NOT NULL THEN
        IF NEW.deleted_by IS DISTINCT FROM actor OR NEW.edited_at IS DISTINCT FROM OLD.edited_at
           OR NOT perm_comment_deletable(OLD.requirement_id, OLD.author_id, actor) THEN
            RAISE EXCEPTION 'you may not delete that comment' USING ERRCODE = 'insufficient_privilege';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.deleted_by IS NOT NULL OR OLD.author_id IS DISTINCT FROM actor
       OR NOT perm_requirement_commentable(OLD.requirement_id, actor) THEN
        RAISE EXCEPTION 'you may not change that comment' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;

-- Resolving and reopening is anybody's who may comment, in their own name; nothing else about a thread moves.
CREATE FUNCTION comment_thread_write_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_user <> 'exciter_app' THEN
        RETURN NEW;
    END IF;
    IF (NEW.requirement_id, NEW.created_by, NEW.created_at) IS DISTINCT FROM (OLD.requirement_id, OLD.created_by, OLD.created_at)
       OR (NEW.resolved_at IS NOT NULL AND NEW.resolved_by IS DISTINCT FROM current_actor_id())
       OR (NEW.resolved_at IS NULL AND NEW.resolved_by IS NOT NULL)
       OR NOT perm_requirement_commentable(OLD.requirement_id, current_actor_id()) THEN
        RAISE EXCEPTION 'you may not change that thread' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;

-- Whether every id an event says it mentions is a member of the installation. A malformed id is no member; the
-- CASE keeps the cast from running on one.
CREATE FUNCTION outbox_mentions_members(payload jsonb) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT NOT (payload ? 'mentioned')
        OR (jsonb_typeof(payload -> 'mentioned') = 'array'
            AND NOT EXISTS (
                SELECT 1 FROM jsonb_array_elements(payload -> 'mentioned') AS m (id)
                WHERE CASE WHEN jsonb_typeof(m.id) = 'string'
                            AND m.id #>> '{}' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                           THEN NOT EXISTS (SELECT 1 FROM app_user u WHERE u.id = (m.id #>> '{}')::uuid AND u.role IS NOT NULL)
                           ELSE true END))
$$;
-- +goose StatementEnd

CREATE TRIGGER comment_revision BEFORE INSERT ON comment
    FOR EACH ROW EXECUTE FUNCTION comment_on_current_revision();
CREATE TRIGGER comment_write_check BEFORE UPDATE ON comment
    FOR EACH ROW EXECUTE FUNCTION comment_write_guard();
CREATE TRIGGER comment_thread_write_check BEFORE UPDATE ON comment_thread
    FOR EACH ROW EXECUTE FUNCTION comment_thread_write_guard();

ALTER TABLE comment_thread ENABLE ROW LEVEL SECURITY;
ALTER TABLE comment_thread FORCE  ROW LEVEL SECURITY;
ALTER TABLE comment        ENABLE ROW LEVEL SECURITY;
ALTER TABLE comment        FORCE  ROW LEVEL SECURITY;

-- One open policy per table, as every table has since #106, and the permission policies beside it.
CREATE POLICY comment_thread_installation ON comment_thread USING (true) WITH CHECK (true);
CREATE POLICY comment_installation ON comment USING (true) WITH CHECK (true);

-- A comment is read with its requirement, and written only in one's own name.
CREATE POLICY comment_thread_viewers ON comment_thread AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY comment_thread_starters ON comment_thread AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND resolved_at IS NULL AND resolved_by IS NULL
                AND perm_requirement_commentable(requirement_id, current_actor_id()));
-- What an update may change is comment_thread_write_guard's to say.
CREATE POLICY comment_thread_resolvers ON comment_thread AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()))
    WITH CHECK (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY comment_viewers ON comment AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY comment_writers ON comment AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (author_id = current_actor_id() AND deleted_at IS NULL AND deleted_by IS NULL AND edited_at IS NULL
                AND perm_requirement_commentable(requirement_id, current_actor_id()));
-- What an update may change is comment_write_guard's to say.
CREATE POLICY comment_changers ON comment AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()))
    WITH CHECK (perm_requirement_viewable(requirement_id, current_actor_id()));

-- A request tells the worker to mention only members, so a forged event cannot reach somebody else.
CREATE POLICY outbox_event_mentions_members ON outbox_event AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (outbox_mentions_members(payload));

-- Nothing is ever deleted outright but with its requirement.
GRANT SELECT, INSERT, UPDATE, DELETE ON comment_thread, comment TO exciter_admin;
REVOKE ALL ON comment_thread, comment FROM exciter_app;
GRANT SELECT, INSERT ON comment_thread, comment TO exciter_app;
GRANT UPDATE (body, edited_at, deleted_at, deleted_by) ON comment TO exciter_app;
GRANT UPDATE (resolved_at, resolved_by) ON comment_thread TO exciter_app;

-- +goose Down
DROP POLICY outbox_event_mentions_members ON outbox_event;
DROP TABLE comment;
DROP TABLE comment_thread;
DROP FUNCTION outbox_mentions_members(jsonb);
DROP FUNCTION comment_thread_write_guard();
DROP FUNCTION comment_write_guard();
DROP FUNCTION comment_on_current_revision();
DROP FUNCTION perm_comment_deletable(uuid, uuid, uuid);
DROP FUNCTION perm_requirement_commentable(uuid, uuid);
