-- +goose Up
-- Requirements traced to Armature issues (#27). A link is a requirement, a relation and an issue key; nothing of
-- the issue is kept but its status category, stored for everyone as docs/decisions.md records.
CREATE TABLE armature_issue_link (
    id              uuid NOT NULL DEFAULT uuidv7(),
    org_id          uuid NOT NULL,
    requirement_id  uuid NOT NULL,
    -- This requirement is implemented by, verified by, or related to the issue.
    relation        text NOT NULL CHECK (relation IN ('implements', 'verifies', 'relates')),
    issue_key       text NOT NULL CHECK (issue_key ~ '^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$'),
    -- Written by the service as the admin role whenever somebody reads the issue live, and marked stale by a
    -- transition it was not told the category of (#29).
    status_category text CHECK (status_category IN ('todo', 'in_progress', 'done')),
    status_stale    boolean NOT NULL DEFAULT false,
    -- Set by the webhook receiver (#29): the issue was deleted, or Armature took the remote link off it.
    broken_at       timestamptz,
    detached_at     timestamptz,
    created_by      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    CONSTRAINT armature_issue_link_once UNIQUE (org_id, requirement_id, issue_key, relation),
    FOREIGN KEY (org_id, requirement_id) REFERENCES requirement (org_id, id) ON DELETE CASCADE
);

CREATE INDEX armature_issue_link_issue_idx ON armature_issue_link (org_id, issue_key);

-- What link sync last did in Armature for one requirement and issue, whatever the relations: one remote link
-- stands per pair. No foreign key to the requirement, so the sync after a purge still finds what to take off.
CREATE TABLE armature_remote_link (
    org_id         uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    requirement_id uuid NOT NULL,
    issue_key      text NOT NULL CHECK (issue_key ~ '^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$'),
    -- Null until Armature made the link, and again once it took it off.
    remote_link_id uuid,
    url            text,
    title          text,
    state          text NOT NULL CHECK (state IN ('synced', 'pending', 'failed')),
    error          text,
    synced_at      timestamptz,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, requirement_id, issue_key),
    CONSTRAINT armature_remote_link_failed_says_why CHECK ((state = 'failed') = (error IS NOT NULL))
);

ALTER TABLE armature_issue_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE armature_issue_link FORCE  ROW LEVEL SECURITY;
ALTER TABLE armature_remote_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE armature_remote_link FORCE  ROW LEVEL SECURITY;

CREATE POLICY armature_issue_link_tenant_isolation ON armature_issue_link
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY armature_issue_link_admin_bypass ON armature_issue_link TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY armature_remote_link_tenant_isolation ON armature_remote_link
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY armature_remote_link_admin_bypass ON armature_remote_link TO exciter_admin USING (true) WITH CHECK (true);

-- A link shows to whoever views its requirement, and is made in one's own name and removed by whoever edits it.
-- Only the worker writes the sync records, which readers of the requirement see.
CREATE POLICY armature_issue_link_visible ON armature_issue_link AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY armature_issue_link_added ON armature_issue_link AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND perm_requirement_editable(requirement_id, current_actor_id()));
CREATE POLICY armature_issue_link_removed ON armature_issue_link AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_requirement_editable(requirement_id, current_actor_id()));
CREATE POLICY armature_remote_link_visible ON armature_remote_link AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));

-- An armature.links event names one requirement and the person whose change it is, nothing else: the worker
-- reads the requirement when it runs, with that person's token.
CREATE POLICY outbox_event_armature_links_shape ON outbox_event AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (topic <> 'armature.links' OR (
        jsonb_typeof(payload -> 'requirementId') = 'string'
        AND payload ->> 'requirementId' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        AND payload - 'requirementId' - 'actorId' = '{}'::jsonb));

-- +goose StatementBegin
-- Only a live requirement of an organization connected to Armature takes a link, and a section none.
CREATE FUNCTION armature_issue_link_open() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM armature_connection WHERE org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'the organization has not connected Armature' USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM requirement WHERE org_id = NEW.org_id AND id = NEW.requirement_id
                   AND kind = 'requirement' AND trashed_at IS NULL) THEN
        RAISE EXCEPTION 'only a requirement out of the trash takes Armature issues' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

-- Tells the worker to sync one requirement's remote links, as the person the transaction acts for. Nothing is
-- written without such a person or a connection, for a requirement with neither links nor sync records, or twice
-- in one transaction.
CREATE FUNCTION armature_links_emit(target uuid) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF actor IS NULL OR current_org_id() IS NULL
       OR NOT EXISTS (SELECT 1 FROM armature_connection WHERE org_id = current_org_id()) THEN
        RETURN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM armature_issue_link WHERE org_id = current_org_id() AND requirement_id = target)
       AND NOT EXISTS (SELECT 1 FROM armature_remote_link WHERE org_id = current_org_id() AND requirement_id = target) THEN
        RETURN;
    END IF;
    IF EXISTS (SELECT 1 FROM outbox_event
               WHERE org_id = current_org_id() AND topic = 'armature.links' AND processed_at IS NULL
                 AND xmin = pg_current_xact_id()::xid AND payload ->> 'requirementId' = target::text) THEN
        RETURN;
    END IF;
    INSERT INTO outbox_event (org_id, topic, payload)
    VALUES (current_org_id(), 'armature.links', jsonb_build_object('requirementId', target, 'actorId', actor));
END;
$$;

CREATE FUNCTION armature_issue_link_changed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM armature_links_emit(CASE WHEN TG_OP = 'DELETE' THEN OLD.requirement_id ELSE NEW.requirement_id END);
    RETURN NULL;
END;
$$;

-- A new revision, title, number, project, trash, restore or purge can change what the issue shows.
CREATE FUNCTION armature_requirement_changed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM armature_links_emit(CASE WHEN TG_OP = 'DELETE' THEN OLD.id ELSE NEW.id END);
    RETURN NULL;
END;
$$;

-- A new project key is a new key for every requirement in it.
CREATE FUNCTION armature_project_rekeyed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM armature_links_emit(l.requirement_id)
    FROM (SELECT DISTINCT a.requirement_id FROM armature_issue_link a
          JOIN requirement r ON r.org_id = a.org_id AND r.id = a.requirement_id
          WHERE a.org_id = NEW.org_id AND r.project_id = NEW.id) l;
    RETURN NULL;
END;
$$;

-- A new address or Armature organization, or none, drops the sync records with the tokens: they describe links
-- in an Armature that is no longer this organization's. The links themselves stay, keyed by issue.
CREATE FUNCTION armature_connection_forgets_links() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_remote_link WHERE org_id = OLD.org_id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER armature_issue_link_open BEFORE INSERT ON armature_issue_link
    FOR EACH ROW EXECUTE FUNCTION armature_issue_link_open();
CREATE TRIGGER armature_issue_link_changed AFTER INSERT OR DELETE ON armature_issue_link
    FOR EACH ROW EXECUTE FUNCTION armature_issue_link_changed();
CREATE TRIGGER armature_requirement_changed AFTER UPDATE ON requirement
    FOR EACH ROW
    WHEN (OLD.revision IS DISTINCT FROM NEW.revision OR OLD.title IS DISTINCT FROM NEW.title
          OR OLD.number IS DISTINCT FROM NEW.number OR OLD.project_id IS DISTINCT FROM NEW.project_id
          OR OLD.trashed_at IS DISTINCT FROM NEW.trashed_at)
    EXECUTE FUNCTION armature_requirement_changed();
CREATE TRIGGER armature_requirement_purged AFTER DELETE ON requirement
    FOR EACH ROW EXECUTE FUNCTION armature_requirement_changed();
CREATE TRIGGER armature_project_rekeyed AFTER UPDATE OF key ON project
    FOR EACH ROW WHEN (OLD.key IS DISTINCT FROM NEW.key)
    EXECUTE FUNCTION armature_project_rekeyed();
CREATE TRIGGER armature_connection_forgets_links AFTER UPDATE OR DELETE ON armature_connection
    FOR EACH ROW EXECUTE FUNCTION armature_connection_forgets_links();

-- Links are made and removed whole; the stored status and the sync records are the server's to write.
GRANT SELECT, INSERT, UPDATE, DELETE ON armature_issue_link, armature_remote_link TO exciter_admin;
REVOKE ALL ON armature_issue_link, armature_remote_link FROM exciter_app;
GRANT SELECT, DELETE ON armature_issue_link TO exciter_app;
GRANT INSERT (org_id, requirement_id, relation, issue_key, created_by) ON armature_issue_link TO exciter_app;
GRANT SELECT ON armature_remote_link TO exciter_app;

-- +goose Down
DROP POLICY IF EXISTS outbox_event_armature_links_shape ON outbox_event;
DROP TRIGGER IF EXISTS armature_connection_forgets_links ON armature_connection;
DROP TRIGGER IF EXISTS armature_project_rekeyed ON project;
DROP TRIGGER IF EXISTS armature_requirement_purged ON requirement;
DROP TRIGGER IF EXISTS armature_requirement_changed ON requirement;
DROP TABLE IF EXISTS armature_remote_link;
DROP TABLE IF EXISTS armature_issue_link;
DROP FUNCTION IF EXISTS armature_connection_forgets_links();
DROP FUNCTION IF EXISTS armature_project_rekeyed();
DROP FUNCTION IF EXISTS armature_requirement_changed();
DROP FUNCTION IF EXISTS armature_issue_link_changed();
DROP FUNCTION IF EXISTS armature_links_emit(uuid);
DROP FUNCTION IF EXISTS armature_issue_link_open();
