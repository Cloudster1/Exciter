-- +goose Up
-- Requirements traced to Stator pages (#41). A link is a requirement, a relation and a page; nothing of the page is
-- kept but its id, its space key, its address and its latest published version, as docs/api-contract-m4.md says.
-- The link watches the page: it is suspect while the page has a newer version than the one last confirmed.
CREATE TABLE stator_page_link (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    requirement_id    uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    -- This requirement is specified by, described by, or related to the page.
    relation          text NOT NULL CHECK (relation IN ('specifies', 'describes', 'relates')),
    page_id           uuid NOT NULL,
    -- Where the page is, written again when Stator says it moved.
    space_key         text NOT NULL CHECK (space_key <> '' AND char_length(space_key) <= 100),
    url               text NOT NULL CHECK (url ~ '^https?://[^[:space:]]+$'),
    -- Stator's word, never the linker's: written by the service as the admin role whenever somebody reads the page
    -- live or Stator announces a version, and the one seen only then and by clearing.
    page_version      integer CHECK (page_version >= 0),
    page_version_seen integer CHECK (page_version_seen >= 0),
    -- The requirement's revision when the link was made or last confirmed.
    reviewed_rev      integer NOT NULL,
    -- Set by the webhook receiver: Stator deleted the page (#41), or a person took the remote link off it (#42).
    broken_at         timestamptz,
    detached_at       timestamptz,
    created_by        uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT stator_page_link_once UNIQUE (requirement_id, page_id, relation)
);

CREATE INDEX stator_page_link_page_idx ON stator_page_link (page_id);

-- +goose StatementBegin
-- Only a live requirement of an installation connected to Stator takes a page, and a section none; a new link is
-- confirmed against the requirement as it is, whatever the writer says.
CREATE FUNCTION stator_page_link_open() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM stator_connection) THEN
        RAISE EXCEPTION 'Exciter is not connected to Stator' USING ERRCODE = 'check_violation';
    END IF;
    SELECT revision INTO NEW.reviewed_rev FROM requirement
    WHERE id = NEW.requirement_id AND kind = 'requirement' AND trashed_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'only a requirement out of the trash takes Stator pages' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER stator_page_link_open BEFORE INSERT ON stator_page_link
    FOR EACH ROW EXECUTE FUNCTION stator_page_link_open();

ALTER TABLE stator_page_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE stator_page_link FORCE  ROW LEVEL SECURITY;
CREATE POLICY stator_page_link_installation ON stator_page_link USING (true) WITH CHECK (true);

-- A link shows to whoever views its requirement, and is made in one's own name and removed by whoever edits it.
CREATE POLICY stator_page_link_visible ON stator_page_link AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
CREATE POLICY stator_page_link_added ON stator_page_link AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND perm_requirement_editable(requirement_id, current_actor_id()));
CREATE POLICY stator_page_link_removed ON stator_page_link AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_requirement_editable(requirement_id, current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON stator_page_link TO exciter_admin;
REVOKE ALL ON stator_page_link FROM exciter_app;
GRANT SELECT, DELETE ON stator_page_link TO exciter_app;
GRANT INSERT (requirement_id, relation, page_id, space_key, url, created_by) ON stator_page_link TO exciter_app;

-- A page link's clearance names the page's version confirmed, beside the requirement's revision as its source.
ALTER TABLE trace_link_clearance
    ADD COLUMN stator_link_id uuid REFERENCES stator_page_link(id) ON DELETE CASCADE,
    ADD COLUMN page_version integer CHECK (page_version >= 0),
    DROP CONSTRAINT trace_link_clearance_of_one,
    ADD CONSTRAINT trace_link_clearance_of_one CHECK (
        num_nonnulls(link_id, armature_link_id, stator_link_id) = 1
        AND (link_id IS NULL) = (target_rev IS NULL)
        AND (stator_link_id IS NULL) = (page_version IS NULL));
CREATE INDEX trace_link_clearance_stator_idx ON trace_link_clearance (stator_link_id) WHERE stator_link_id IS NOT NULL;

ALTER POLICY trace_link_clearance_readers ON trace_link_clearance
    USING ((link_id IS NOT NULL AND EXISTS (SELECT 1 FROM trace_link l WHERE l.id = link_id))
           OR (armature_link_id IS NOT NULL AND EXISTS (SELECT 1 FROM armature_issue_link a WHERE a.id = armature_link_id))
           OR (stator_link_id IS NOT NULL AND EXISTS (SELECT 1 FROM stator_page_link p WHERE p.id = stator_link_id)));

-- +goose StatementBegin
-- Clearing a page link as the person the transaction acts for, who may edit its requirement: it is confirmed
-- against the page's version they read, which has to be the latest Exciter knows, and the requirement as it is now.
CREATE FUNCTION stator_page_link_clear(link uuid, named integer, why text) RETURNS uuid
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor   uuid := current_actor_id();
    l       stator_page_link;
    r       requirement;
    cleared uuid;
BEGIN
    SELECT * INTO l FROM stator_page_link WHERE id = link FOR UPDATE;
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
    IF l.page_version IS DISTINCT FROM named THEN
        RAISE EXCEPTION 'a page link is cleared against the page''s latest version'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_clear_current';
    END IF;
    INSERT INTO trace_link_clearance (stator_link_id, source_rev, page_version, comment, cleared_by)
    VALUES (l.id, r.revision, named, btrim(why), actor)
    RETURNING id INTO cleared;
    UPDATE stator_page_link SET page_version_seen = named, reviewed_rev = r.revision WHERE id = l.id;
    RETURN cleared;
END;
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION stator_page_link_clear(uuid, integer, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION stator_page_link_clear(uuid, integer, text) TO exciter_app;

-- +goose Down
DROP FUNCTION stator_page_link_clear(uuid, integer, text);
DELETE FROM trace_link_clearance WHERE stator_link_id IS NOT NULL;
ALTER POLICY trace_link_clearance_readers ON trace_link_clearance
    USING ((link_id IS NOT NULL AND EXISTS (SELECT 1 FROM trace_link l WHERE l.id = link_id))
           OR (armature_link_id IS NOT NULL AND EXISTS (SELECT 1 FROM armature_issue_link a WHERE a.id = armature_link_id)));
ALTER TABLE trace_link_clearance
    DROP CONSTRAINT trace_link_clearance_of_one,
    DROP COLUMN page_version,
    DROP COLUMN stator_link_id,
    ADD CONSTRAINT trace_link_clearance_of_one CHECK (
        (link_id IS NULL) <> (armature_link_id IS NULL) AND (link_id IS NULL) = (target_rev IS NULL));
DROP TABLE stator_page_link;
DROP FUNCTION stator_page_link_open();
