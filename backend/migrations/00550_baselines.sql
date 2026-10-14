-- +goose Up
-- Baselines (#37): a named snapshot of a project, or of one section and everything below it. An item names a
-- requirement's revision and records where it stood and in which status; nothing of its content is copied. The
-- items, links and external links are written once, in the transaction that takes the baseline, and never change.

CREATE TABLE baseline (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id  uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    -- The section the baseline is of, null for the whole project. No foreign key: a baseline outlives the purge of
    -- its section, which it then names by number.
    root_id     uuid,
    root_number integer,
    -- The transaction that took it, the only one that writes its items.
    taken_xact  xid8 NOT NULL DEFAULT pg_current_xact_id(),
    locked_at   timestamptz,
    locked_by   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT baseline_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT baseline_description_shape CHECK (char_length(description) <= 2000),
    CONSTRAINT baseline_root_named CHECK ((root_id IS NULL) = (root_number IS NULL))
);

CREATE UNIQUE INDEX baseline_name_taken ON baseline (project_id, lower(name));

-- Each requirement and section as the baseline holds it. No foreign keys to the requirement or its revision: a
-- requirement purged since is still named by its number, its content gone with it.
CREATE TABLE baseline_item (
    baseline_id     uuid NOT NULL REFERENCES baseline(id) ON DELETE CASCADE,
    requirement_id  uuid NOT NULL,
    number          integer NOT NULL,
    kind            text NOT NULL CHECK (kind IN ('requirement', 'section')),
    revision        integer NOT NULL CHECK (revision > 0),
    parent_id       uuid,
    rank            text COLLATE "C" NOT NULL,
    -- The place in the outline as it was, which numbers the item and orders the baseline.
    path            integer[] NOT NULL,
    status_id       uuid,
    status_name     text,
    status_category text,
    PRIMARY KEY (baseline_id, requirement_id)
);

CREATE INDEX baseline_item_order_idx ON baseline_item (baseline_id, path);

CREATE TABLE baseline_link (
    baseline_id uuid NOT NULL REFERENCES baseline(id) ON DELETE CASCADE,
    link_id     uuid NOT NULL,
    type_id     uuid NOT NULL,
    type_name   text NOT NULL,
    source_id   uuid NOT NULL,
    target_id   uuid NOT NULL,
    PRIMARY KEY (baseline_id, link_id)
);

CREATE TABLE baseline_external_link (
    baseline_id    uuid NOT NULL REFERENCES baseline(id) ON DELETE CASCADE,
    link_id        uuid NOT NULL,
    requirement_id uuid NOT NULL,
    issue_key      text NOT NULL,
    relation       text NOT NULL,
    PRIMARY KEY (baseline_id, link_id)
);

-- +goose StatementBegin
-- A baseline keeps its project, root and transaction; a locked one is locked for good and keeps its name and
-- description; only an unlocked one is deleted, unless its project goes, which cascades from deeper down.
CREATE FUNCTION baseline_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.locked_at IS NOT NULL AND pg_trigger_depth() = 1 THEN
            RAISE EXCEPTION 'a locked baseline is never deleted'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_locked';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.project_id <> OLD.project_id OR NEW.root_id IS DISTINCT FROM OLD.root_id
       OR NEW.root_number IS DISTINCT FROM OLD.root_number OR NEW.taken_xact <> OLD.taken_xact
       OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'a baseline keeps what it was taken of'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_fixed';
    END IF;
    IF OLD.locked_at IS NOT NULL THEN
        RAISE EXCEPTION 'a locked baseline does not change'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_locked';
    END IF;
    RETURN NEW;
END;
$$;

-- What a baseline holds is written in the transaction that took it and never changed; it goes only with its
-- baseline.
CREATE FUNCTION baseline_rows_frozen() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    b baseline;
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF pg_trigger_depth() = 1 THEN
            RAISE EXCEPTION 'what a baseline holds goes only with it'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_frozen';
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'what a baseline holds never changes'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_frozen';
    END IF;
    SELECT * INTO b FROM baseline WHERE id = NEW.baseline_id;
    IF b.taken_xact <> pg_current_xact_id() THEN
        RAISE EXCEPTION 'a baseline is filled only as it is taken'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_frozen';
    END IF;
    RETURN NEW;
END;
$$;

-- An item names a revision a requirement of the baseline's project has.
CREATE FUNCTION baseline_item_held() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM baseline b
        JOIN requirement r ON r.project_id = b.project_id
        JOIN requirement_revision v ON v.requirement_id = r.id AND v.number = NEW.revision
        WHERE b.id = NEW.baseline_id AND r.id = NEW.requirement_id AND r.number = NEW.number) THEN
        RAISE EXCEPTION 'a baseline holds revisions its project''s requirements have'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'baseline_item_revision';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER baseline_guard BEFORE UPDATE OR DELETE ON baseline
    FOR EACH ROW EXECUTE FUNCTION baseline_guard();
CREATE TRIGGER baseline_item_frozen BEFORE INSERT OR UPDATE OR DELETE ON baseline_item
    FOR EACH ROW EXECUTE FUNCTION baseline_rows_frozen();
CREATE TRIGGER baseline_item_held BEFORE INSERT ON baseline_item
    FOR EACH ROW EXECUTE FUNCTION baseline_item_held();
CREATE TRIGGER baseline_link_frozen BEFORE INSERT OR UPDATE OR DELETE ON baseline_link
    FOR EACH ROW EXECUTE FUNCTION baseline_rows_frozen();
CREATE TRIGGER baseline_external_link_frozen BEFORE INSERT OR UPDATE OR DELETE ON baseline_external_link
    FOR EACH ROW EXECUTE FUNCTION baseline_rows_frozen();

ALTER TABLE baseline               ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline               FORCE  ROW LEVEL SECURITY;
ALTER TABLE baseline_item          ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline_item          FORCE  ROW LEVEL SECURITY;
ALTER TABLE baseline_link          ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline_link          FORCE  ROW LEVEL SECURITY;
ALTER TABLE baseline_external_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline_external_link FORCE  ROW LEVEL SECURITY;

CREATE POLICY baseline_installation ON baseline USING (true) WITH CHECK (true);
CREATE POLICY baseline_item_installation ON baseline_item USING (true) WITH CHECK (true);
CREATE POLICY baseline_link_installation ON baseline_link USING (true) WITH CHECK (true);
CREATE POLICY baseline_external_link_installation ON baseline_external_link USING (true) WITH CHECK (true);

-- A project's readers read its baselines; its administrators take, rename, lock and delete them while it is open.
CREATE POLICY baseline_readers ON baseline AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY baseline_takers ON baseline AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND perm_project_holds(project_id, current_actor_id(), 'administer')
                AND EXISTS (SELECT 1 FROM project p WHERE p.id = project_id AND p.archived_at IS NULL));
CREATE POLICY baseline_keepers ON baseline AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer'))
    WITH CHECK (EXISTS (SELECT 1 FROM project p WHERE p.id = project_id AND p.archived_at IS NULL));
CREATE POLICY baseline_removers ON baseline AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer')
           AND EXISTS (SELECT 1 FROM project p WHERE p.id = project_id AND p.archived_at IS NULL));

-- What a baseline holds shows with it, and is written by whoever took it; the subqueries read as the reader.
CREATE POLICY baseline_item_readers ON baseline_item AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id));
CREATE POLICY baseline_item_takers ON baseline_item AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id AND b.created_by = current_actor_id()));
CREATE POLICY baseline_link_readers ON baseline_link AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id));
CREATE POLICY baseline_link_takers ON baseline_link AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id AND b.created_by = current_actor_id()));
CREATE POLICY baseline_external_link_readers ON baseline_external_link AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id));
CREATE POLICY baseline_external_link_takers ON baseline_external_link AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id AND b.created_by = current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON baseline, baseline_item, baseline_link, baseline_external_link TO exciter_admin;
REVOKE ALL ON baseline, baseline_item, baseline_link, baseline_external_link FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON baseline TO exciter_app;
GRANT UPDATE (name, description, locked_at, locked_by) ON baseline TO exciter_app;
GRANT SELECT, INSERT ON baseline_item, baseline_link, baseline_external_link TO exciter_app;

-- +goose Down
DROP TABLE baseline_external_link;
DROP TABLE baseline_link;
DROP TABLE baseline_item;
DROP TABLE baseline;
DROP FUNCTION baseline_item_held();
DROP FUNCTION baseline_rows_frozen();
DROP FUNCTION baseline_guard();
