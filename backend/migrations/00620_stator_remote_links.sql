-- +goose Up
-- The way back (#42): the worker puts each requirement on the Stator pages it is traced to, as remote links, as
-- Armature's link sync does on issues (00310, as it stands since 00415). docs/api-contract-m4.md says how.

-- What link sync last did on one page for one requirement, whatever the relations: one remote link stands per pair.
-- No foreign key to the requirement, so the sync after a purge still finds what to take off.
CREATE TABLE stator_remote_link (
    requirement_id uuid NOT NULL,
    page_id        uuid NOT NULL,
    -- Null until Stator made the link, and again once it took it off.
    remote_link_id uuid,
    url            text,
    title          text,
    state          text NOT NULL CHECK (state IN ('synced', 'pending', 'failed')),
    error          text,
    synced_at      timestamptz,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (requirement_id, page_id),
    CONSTRAINT stator_remote_link_failed_says_why CHECK ((state = 'failed') = (error IS NOT NULL))
);

CREATE INDEX stator_remote_link_page_idx ON stator_remote_link (page_id);

ALTER TABLE stator_remote_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE stator_remote_link FORCE  ROW LEVEL SECURITY;
CREATE POLICY stator_remote_link_installation ON stator_remote_link USING (true) WITH CHECK (true);
-- Only the worker writes the sync records, which readers of the requirement see.
CREATE POLICY stator_remote_link_visible ON stator_remote_link AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON stator_remote_link TO exciter_admin;
REVOKE ALL ON stator_remote_link FROM exciter_app;
GRANT SELECT ON stator_remote_link TO exciter_app;

-- A stator.links event names one requirement and the person whose change it is, nothing else: the worker reads
-- the requirement when it runs, with that person's token.
CREATE POLICY outbox_event_stator_links_shape ON outbox_event AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (topic <> 'stator.links' OR (
        jsonb_typeof(payload -> 'requirementId') = 'string'
        AND payload ->> 'requirementId' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        AND payload - 'requirementId' - 'actorId' = '{}'::jsonb));

-- +goose StatementBegin
-- Tells the worker to sync one requirement's remote links, as the person the transaction acts for. Nothing is
-- written without such a person, while Stator takes no remote links, for a requirement with neither page links
-- nor sync records, or twice in one transaction.
CREATE FUNCTION stator_links_emit(target uuid) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF actor IS NULL OR NOT EXISTS (SELECT 1 FROM stator_connection WHERE remote_links) THEN
        RETURN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM stator_page_link WHERE requirement_id = target)
       AND NOT EXISTS (SELECT 1 FROM stator_remote_link WHERE requirement_id = target) THEN
        RETURN;
    END IF;
    IF EXISTS (SELECT 1 FROM outbox_event
               WHERE topic = 'stator.links' AND processed_at IS NULL
                 AND xmin = pg_current_xact_id()::xid AND payload ->> 'requirementId' = target::text) THEN
        RETURN;
    END IF;
    INSERT INTO outbox_event (topic, payload)
    VALUES ('stator.links', jsonb_build_object('requirementId', target, 'actorId', actor));
END;
$$;

CREATE FUNCTION stator_page_link_changed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM stator_links_emit(CASE WHEN TG_OP = 'DELETE' THEN OLD.requirement_id ELSE NEW.requirement_id END);
    RETURN NULL;
END;
$$;

-- A new revision, title, number, project, trash, restore or purge can change what the page shows.
CREATE FUNCTION stator_requirement_changed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM stator_links_emit(CASE WHEN TG_OP = 'DELETE' THEN OLD.id ELSE NEW.id END);
    RETURN NULL;
END;
$$;

-- A new project key is a new key for every requirement in it.
CREATE FUNCTION stator_project_rekeyed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM stator_links_emit(l.requirement_id)
    FROM (SELECT DISTINCT s.requirement_id FROM stator_page_link s
          JOIN requirement r ON r.id = s.requirement_id
          WHERE r.project_id = NEW.id) l;
    RETURN NULL;
END;
$$;

-- A new address, or none, drops the sync records: they describe links in a Stator that is no longer the
-- installation's. Stator newly taking remote links puts every live link there, each requirement's as the maker of
-- its newest one, so a page shows only what somebody who chose it would have put.
CREATE FUNCTION stator_connection_syncs_links() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.base_url IS DISTINCT FROM OLD.base_url THEN
        DELETE FROM stator_remote_link;
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.remote_links AND NOT OLD.remote_links THEN
        INSERT INTO outbox_event (topic, payload)
        SELECT 'stator.links', jsonb_build_object('requirementId', l.requirement_id, 'actorId', l.created_by)
        FROM (SELECT DISTINCT ON (requirement_id) requirement_id, created_by
              FROM stator_page_link
              WHERE created_by IS NOT NULL AND broken_at IS NULL AND detached_at IS NULL
              ORDER BY requirement_id, created_at DESC, id DESC) l;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER stator_page_link_changed AFTER INSERT OR DELETE ON stator_page_link
    FOR EACH ROW EXECUTE FUNCTION stator_page_link_changed();
CREATE TRIGGER stator_requirement_changed AFTER UPDATE ON requirement
    FOR EACH ROW
    WHEN (OLD.revision IS DISTINCT FROM NEW.revision OR OLD.title IS DISTINCT FROM NEW.title
          OR OLD.number IS DISTINCT FROM NEW.number OR OLD.project_id IS DISTINCT FROM NEW.project_id
          OR OLD.trashed_at IS DISTINCT FROM NEW.trashed_at)
    EXECUTE FUNCTION stator_requirement_changed();
CREATE TRIGGER stator_requirement_purged AFTER DELETE ON requirement
    FOR EACH ROW EXECUTE FUNCTION stator_requirement_changed();
CREATE TRIGGER stator_project_rekeyed AFTER UPDATE OF key ON project
    FOR EACH ROW WHEN (OLD.key IS DISTINCT FROM NEW.key)
    EXECUTE FUNCTION stator_project_rekeyed();
CREATE TRIGGER stator_connection_syncs_links AFTER UPDATE OR DELETE ON stator_connection
    FOR EACH ROW EXECUTE FUNCTION stator_connection_syncs_links();

-- +goose Down
DROP TRIGGER stator_connection_syncs_links ON stator_connection;
DROP TRIGGER stator_project_rekeyed ON project;
DROP TRIGGER stator_requirement_purged ON requirement;
DROP TRIGGER stator_requirement_changed ON requirement;
DROP TRIGGER stator_page_link_changed ON stator_page_link;
DROP FUNCTION stator_connection_syncs_links();
DROP FUNCTION stator_project_rekeyed();
DROP FUNCTION stator_requirement_changed();
DROP FUNCTION stator_page_link_changed();
DROP FUNCTION stator_links_emit(uuid);
DROP POLICY outbox_event_stator_links_shape ON outbox_event;
DELETE FROM outbox_event WHERE topic = 'stator.links';
DROP TABLE stator_remote_link;
