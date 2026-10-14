-- +goose Up
-- Who follows what (#34), Stator's 00151, 00152 and 00153 over requirements. A watch is on one requirement or on a
-- whole project; which project a requirement is in is read when an event is delivered, so a moved requirement is
-- covered by its new project's watchers.
CREATE TABLE watch (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id        uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    kind           text NOT NULL CHECK (kind IN ('requirement', 'project')),
    requirement_id uuid REFERENCES requirement(id) ON DELETE CASCADE,
    project_id     uuid REFERENCES project(id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT watch_target CHECK (
        (kind = 'requirement' AND requirement_id IS NOT NULL AND project_id IS NULL) OR
        (kind = 'project' AND project_id IS NOT NULL AND requirement_id IS NULL))
);

-- One own watch per requirement and per project.
CREATE UNIQUE INDEX watch_one_per_requirement_idx ON watch (user_id, requirement_id) WHERE requirement_id IS NOT NULL;
CREATE UNIQUE INDEX watch_one_per_project_idx ON watch (user_id, project_id) WHERE project_id IS NOT NULL;
-- Delivery looks watches up by what they are on.
CREATE INDEX watch_requirement_idx ON watch (requirement_id) WHERE requirement_id IS NOT NULL;
CREATE INDEX watch_project_idx ON watch (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX watch_latest_idx ON watch (user_id, created_at DESC, id DESC);

-- Somebody who stopped watching a requirement: their own edits no longer make them watch it again, until they watch
-- it explicitly.
CREATE TABLE watch_optout (
    user_id        uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    requirement_id uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, requirement_id)
);

-- What a person was told: one row per person per event, which makes a second delivery of the same event a no-op.
-- The row holds facts, not words: the client words it, and the requirement's title is read as it is now. Mail is a
-- copy of a row, never the record itself. Reviews (#35) and links (#36) tie review_id and link_id to their tables.
CREATE TABLE notification (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id        uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    event_id       uuid NOT NULL,
    kind           text NOT NULL CHECK (kind IN ('mentioned', 'review_requested', 'reviewed', 'suspect', 'replied',
                                                 'commented', 'resolved', 'revised', 'transitioned')),
    actor_id       uuid REFERENCES app_user(id) ON DELETE SET NULL,
    requirement_id uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    revision       integer NOT NULL,
    thread_id      uuid REFERENCES comment_thread(id) ON DELETE CASCADE,
    comment_id     uuid REFERENCES comment(id) ON DELETE CASCADE,
    review_id      uuid,
    link_id        uuid,
    excerpt        text NOT NULL DEFAULT '' CHECK (char_length(excerpt) <= 200),
    created_at     timestamptz NOT NULL DEFAULT now(),
    read_at        timestamptz,
    CONSTRAINT notification_once_per_event UNIQUE (event_id, user_id)
);

CREATE INDEX notification_latest_idx ON notification (user_id, created_at DESC, id DESC);
CREATE INDEX notification_unread_idx ON notification (user_id) WHERE read_at IS NULL;
CREATE INDEX notification_requirement_idx ON notification (requirement_id);
CREATE INDEX notification_comment_idx ON notification (comment_id) WHERE comment_id IS NOT NULL;

-- How a person wants to hear. A kind missing from a switch map is on, so a kind added later reaches everybody until
-- they say otherwise; no row at all is everything on, no digest, and watching what one writes.
CREATE TABLE notification_preference (
    user_id    uuid PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,
    in_app     jsonb NOT NULL DEFAULT '{}'::jsonb,
    email      jsonb NOT NULL DEFAULT '{}'::jsonb,
    digest     text NOT NULL DEFAULT 'off' CHECK (digest IN ('off', 'hourly', 'daily')),
    auto_watch boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notification_preference_objects CHECK (jsonb_typeof(in_app) = 'object' AND jsonb_typeof(email) = 'object')
);

-- Rows waiting to be bundled into one mail.
CREATE TABLE notification_digest (
    user_id         uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    notification_id uuid NOT NULL REFERENCES notification(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, notification_id)
);

-- +goose StatementBegin
-- Whether the actor may view what a watch is on.
CREATE FUNCTION watch_target_viewable(requirement uuid, project uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT CASE WHEN requirement IS NOT NULL THEN perm_requirement_viewable(requirement, actor)
                ELSE perm_project_holds(project, actor, 'view') END
$$;

-- Every member whose watch covers a requirement, through their own watch on it or their project's. Nobody's
-- visibility is judged here; the caller does that for each person. Read across people, so it is the worker's alone.
CREATE FUNCTION requirement_watch_coverage(target uuid)
    RETURNS TABLE (user_id uuid, via text)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT DISTINCT ON (f.user_id) f.user_id, f.via
    FROM (
        SELECT w.user_id, 'requirement' AS via, 0 AS rank FROM watch w WHERE w.requirement_id = target
        UNION ALL
        SELECT w.user_id, 'project', 1 FROM watch w JOIN requirement r ON r.project_id = w.project_id WHERE r.id = target
    ) f
    JOIN app_user u ON u.id = f.user_id AND u.is_active AND u.role IS NOT NULL
    ORDER BY f.user_id, f.rank
$$;

-- Who hears about a requirement, for somebody who may view it: only people who may view it themselves.
CREATE FUNCTION requirement_watchers(target uuid)
    RETURNS TABLE (user_id uuid, via text)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT c.user_id, c.via
    FROM requirement_watch_coverage(target) c
    WHERE perm_requirement_viewable(target, current_actor_id())
      AND perm_requirement_viewable(target, c.user_id)
$$;

-- Makes a person watch a requirement they wrote, revised or were made the owner of, unless they watch it already,
-- stopped watching it, switched auto watch off, or may not view it. The person is often not the one acting, so this
-- runs as the table's owner.
CREATE FUNCTION watch_auto(person uuid, target uuid) RETURNS void
    LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    INSERT INTO watch (user_id, kind, requirement_id)
    SELECT person, 'requirement', target
    WHERE person IS NOT NULL
      AND COALESCE((SELECT auto_watch FROM notification_preference WHERE user_id = person), true)
      AND NOT EXISTS (SELECT 1 FROM watch_optout WHERE user_id = person AND requirement_id = target)
      AND EXISTS (SELECT 1 FROM app_user WHERE id = person AND is_active AND role IS NOT NULL)
      AND perm_requirement_viewable(target, person)
    ON CONFLICT DO NOTHING
$$;

CREATE FUNCTION watch_auto_revision() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM watch_auto(NEW.created_by, NEW.requirement_id);
    RETURN NULL;
END;
$$;

CREATE FUNCTION watch_auto_owner() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM watch_auto(NEW.owner_id, NEW.id);
    RETURN NULL;
END;
$$;

-- Deleting a comment takes back what people were told about it. The app role may not delete notifications, and the
-- people told are others, so this runs as the table's owner, and only ever for a comment just deleted.
CREATE FUNCTION comment_forget_notifications() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM notification WHERE comment_id = NEW.id;
    RETURN NULL;
END;
$$;

-- The worker may be about to tell somebody of a comment its author is deleting. The share lock makes the two wait
-- for each other: a delete that committed first leaves nothing to insert, and one that commits after finds the row
-- to take back.
CREATE FUNCTION notification_comment_alive() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.comment_id IS NULL THEN
        RETURN NEW;
    END IF;
    PERFORM 1 FROM comment c WHERE c.id = NEW.comment_id AND c.deleted_at IS NULL FOR SHARE;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION requirement_watch_coverage(uuid) FROM PUBLIC, exciter_app;
GRANT EXECUTE ON FUNCTION requirement_watch_coverage(uuid) TO exciter_admin;
REVOKE EXECUTE ON FUNCTION watch_auto(uuid, uuid) FROM PUBLIC, exciter_app;

CREATE TRIGGER requirement_revision_watch AFTER INSERT ON requirement_revision
    FOR EACH ROW EXECUTE FUNCTION watch_auto_revision();
CREATE TRIGGER requirement_owner_watch AFTER INSERT ON requirement
    FOR EACH ROW WHEN (NEW.owner_id IS NOT NULL) EXECUTE FUNCTION watch_auto_owner();
CREATE TRIGGER requirement_new_owner_watch AFTER UPDATE OF owner_id ON requirement
    FOR EACH ROW WHEN (NEW.owner_id IS NOT NULL AND NEW.owner_id IS DISTINCT FROM OLD.owner_id)
    EXECUTE FUNCTION watch_auto_owner();
CREATE TRIGGER comment_deleted_forget AFTER UPDATE OF deleted_at ON comment
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION comment_forget_notifications();
CREATE TRIGGER notification_about_live_comment BEFORE INSERT ON notification
    FOR EACH ROW EXECUTE FUNCTION notification_comment_alive();

ALTER TABLE watch                   ENABLE ROW LEVEL SECURITY;
ALTER TABLE watch                   FORCE  ROW LEVEL SECURITY;
ALTER TABLE watch_optout            ENABLE ROW LEVEL SECURITY;
ALTER TABLE watch_optout            FORCE  ROW LEVEL SECURITY;
ALTER TABLE notification            ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification            FORCE  ROW LEVEL SECURITY;
ALTER TABLE notification_preference ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_preference FORCE  ROW LEVEL SECURITY;
ALTER TABLE notification_digest     ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_digest     FORCE  ROW LEVEL SECURITY;

-- One open policy per table, as every table has since #106, and the permission policies beside it.
CREATE POLICY watch_installation ON watch USING (true) WITH CHECK (true);
CREATE POLICY watch_optout_installation ON watch_optout USING (true) WITH CHECK (true);
CREATE POLICY notification_installation ON notification USING (true) WITH CHECK (true);
CREATE POLICY notification_preference_installation ON notification_preference USING (true) WITH CHECK (true);
CREATE POLICY notification_digest_installation ON notification_digest USING (true) WITH CHECK (true);

-- Watching is only ever for oneself, and only of what one may view.
CREATE POLICY watch_own ON watch AS RESTRICTIVE FOR ALL TO exciter_app
    USING (user_id = current_actor_id() AND watch_target_viewable(requirement_id, project_id, current_actor_id()))
    WITH CHECK (user_id = current_actor_id() AND watch_target_viewable(requirement_id, project_id, current_actor_id()));
CREATE POLICY watch_optout_own ON watch_optout AS RESTRICTIVE FOR ALL TO exciter_app
    USING (user_id = current_actor_id() AND perm_requirement_viewable(requirement_id, current_actor_id()))
    WITH CHECK (user_id = current_actor_id() AND perm_requirement_viewable(requirement_id, current_actor_id()));

-- A person reads and marks only their own rows, and only about requirements they may still view. The worker writes
-- each row acting for its recipient, so a row about a requirement the recipient may not view cannot be written.
CREATE POLICY notification_own_read ON notification AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (user_id = current_actor_id() AND perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY notification_own_write ON notification AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (user_id = current_actor_id() AND read_at IS NULL
                AND perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY notification_own_mark ON notification AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (user_id = current_actor_id()) WITH CHECK (user_id = current_actor_id());
CREATE POLICY notification_preference_own ON notification_preference AS RESTRICTIVE FOR ALL TO exciter_app
    USING (user_id = current_actor_id()) WITH CHECK (user_id = current_actor_id());
CREATE POLICY notification_digest_own ON notification_digest AS RESTRICTIVE FOR ALL TO exciter_app
    USING (user_id = current_actor_id()) WITH CHECK (user_id = current_actor_id());

-- Marking read is the one change a person makes to a row they were told.
GRANT SELECT, INSERT, UPDATE, DELETE ON watch, watch_optout, notification, notification_preference, notification_digest
    TO exciter_admin;
REVOKE ALL ON watch, watch_optout, notification, notification_preference, notification_digest FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON watch, watch_optout TO exciter_app;
GRANT SELECT, INSERT ON notification TO exciter_app;
GRANT UPDATE (read_at) ON notification TO exciter_app;
GRANT SELECT, INSERT, UPDATE ON notification_preference TO exciter_app;
GRANT SELECT, INSERT, DELETE ON notification_digest TO exciter_app;

-- +goose Down
-- The policies call watch_target_viewable, so the tables go before the functions.
DROP TRIGGER comment_deleted_forget ON comment;
DROP TRIGGER requirement_new_owner_watch ON requirement;
DROP TRIGGER requirement_owner_watch ON requirement;
DROP TRIGGER requirement_revision_watch ON requirement_revision;
DROP TABLE notification_digest;
DROP TABLE notification_preference;
DROP TABLE notification;
DROP TABLE watch_optout;
DROP TABLE watch;
DROP FUNCTION notification_comment_alive();
DROP FUNCTION comment_forget_notifications();
DROP FUNCTION watch_auto_owner();
DROP FUNCTION watch_auto_revision();
DROP FUNCTION watch_auto(uuid, uuid);
DROP FUNCTION requirement_watchers(uuid);
DROP FUNCTION requirement_watch_coverage(uuid);
DROP FUNCTION watch_target_viewable(uuid, uuid, uuid);
