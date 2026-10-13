-- +goose Up
-- Trace links (#18): typed, directed links between requirements, read from their source ("CHK-R7 derives from
-- SYS-R2"). The organization keeps link types every project may use, and a project may disable one, make its
-- own, and limit a type to pairs of requirement types. A link records the revision of each end it was made
-- against, and its removal leaves a tombstone that baselines and history read.
CREATE TABLE link_type (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    -- Null is the organization's, for every project.
    project_id  uuid,
    key         text NOT NULL,
    name        text NOT NULL,
    forward     text NOT NULL,
    backward    text NOT NULL,
    acyclic     boolean NOT NULL DEFAULT false,
    suspect_end text NOT NULL DEFAULT 'none' CHECK (suspect_end IN ('target', 'source', 'both', 'none')),
    counts      text NOT NULL DEFAULT 'nothing' CHECK (counts IN ('decomposition', 'verification', 'nothing')),
    retired     boolean NOT NULL DEFAULT false,
    created_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT link_type_org_id_key UNIQUE (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    CONSTRAINT link_type_key_shape CHECK (key ~ '^[a-z][a-z0-9_]{1,39}$'),
    CONSTRAINT link_type_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 40),
    CONSTRAINT link_type_phrase_shape CHECK (
        btrim(forward) <> '' AND char_length(forward) <= 60 AND btrim(backward) <> '' AND char_length(backward) <= 60)
);

CREATE UNIQUE INDEX link_type_org_key_idx ON link_type (org_id, key) WHERE project_id IS NULL;
CREATE UNIQUE INDEX link_type_project_key_idx ON link_type (project_id, key) WHERE project_id IS NOT NULL;
CREATE UNIQUE INDEX link_type_org_name_idx ON link_type (org_id, lower(name)) WHERE project_id IS NULL;
CREATE UNIQUE INDEX link_type_project_name_idx ON link_type (project_id, lower(name)) WHERE project_id IS NOT NULL;

CREATE TRIGGER link_type_set_updated_at BEFORE UPDATE ON link_type
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A project sees the organization's types beside its own, so a key or a name is taken across both; the
-- indexes hold each scope, and this the two together, under a lock so two makers cannot pass it at once. The
-- key names a type in imports and scripts, so it never changes, nor does whose a type is.
-- +goose StatementBegin
CREATE FUNCTION link_type_names_free() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND (NEW.key <> OLD.key OR NEW.project_id IS DISTINCT FROM OLD.project_id) THEN
        RAISE EXCEPTION 'a link type keeps its key and its project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_key_fixed';
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended('link_type:' || NEW.org_id::text, 0));
    IF EXISTS (SELECT 1 FROM link_type t
               WHERE t.org_id = NEW.org_id AND t.id <> NEW.id AND t.key = NEW.key
                 AND (NEW.project_id IS NULL) <> (t.project_id IS NULL)) THEN
        RAISE EXCEPTION 'the link type key % is taken', NEW.key
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'link_type_key_taken';
    END IF;
    IF EXISTS (SELECT 1 FROM link_type t
               WHERE t.org_id = NEW.org_id AND t.id <> NEW.id AND lower(t.name) = lower(NEW.name)
                 AND (NEW.project_id IS NULL) <> (t.project_id IS NULL)) THEN
        RAISE EXCEPTION 'the link type name % is taken', NEW.name
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'link_type_name_taken';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER link_type_names_free BEFORE INSERT OR UPDATE OF key, name, project_id ON link_type
    FOR EACH ROW EXECUTE FUNCTION link_type_names_free();

-- Whether a project uses a type; no row is enabled, so a new type of the organization reaches every project.
CREATE TABLE link_type_project (
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id   uuid NOT NULL,
    link_type_id uuid NOT NULL,
    enabled      boolean NOT NULL,
    PRIMARY KEY (project_id, link_type_id),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, link_type_id) REFERENCES link_type (org_id, id) ON DELETE CASCADE
);

-- The pairs of requirement types a type joins in a project, a null end any type; none is any pair.
CREATE TABLE link_type_rule (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id         uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id     uuid NOT NULL,
    link_type_id   uuid NOT NULL,
    source_type_id uuid,
    target_type_id uuid,
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, link_type_id) REFERENCES link_type (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, source_type_id) REFERENCES requirement_type (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, target_type_id) REFERENCES requirement_type (org_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX link_type_rule_pair_idx ON link_type_rule (project_id, link_type_id, source_type_id, target_type_id) NULLS NOT DISTINCT;

-- A project sets only types it may use, the organization's and its own, and names only its own requirement
-- types; a policy cannot see that, since it would read them through the writer's own rights.
-- +goose StatementBegin
CREATE FUNCTION link_type_project_reach() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM link_type t WHERE t.org_id = NEW.org_id AND t.id = NEW.link_type_id
                   AND (t.project_id IS NULL OR t.project_id = NEW.project_id)) THEN
        RAISE EXCEPTION 'a project sets the organization''s link types and its own'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_project_reach';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION link_type_rule_reach() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM requirement_type rt
               WHERE rt.org_id = NEW.org_id AND rt.id IN (NEW.source_type_id, NEW.target_type_id) AND rt.project_id <> NEW.project_id) THEN
        RAISE EXCEPTION 'a link type rule names requirement types of its own project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'link_type_project_reach';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER link_type_project_reach BEFORE INSERT OR UPDATE ON link_type_project
    FOR EACH ROW EXECUTE FUNCTION link_type_project_reach();
CREATE TRIGGER link_type_rule_reach BEFORE INSERT OR UPDATE ON link_type_rule
    FOR EACH ROW EXECUTE FUNCTION link_type_project_reach();
CREATE TRIGGER link_type_rule_types BEFORE INSERT OR UPDATE ON link_type_rule
    FOR EACH ROW EXECUTE FUNCTION link_type_rule_reach();

CREATE TABLE trace_link (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id              uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    type_id             uuid NOT NULL,
    source_id           uuid NOT NULL,
    target_id           uuid NOT NULL,
    -- The revision of each end the link was made against, which #36 compares with the ends' to call it suspect.
    source_reviewed_rev integer NOT NULL DEFAULT 0,
    target_reviewed_rev integer NOT NULL DEFAULT 0,
    created_by          uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    -- A removed link stays as a tombstone.
    deleted_at          timestamptz,
    deleted_by          uuid REFERENCES app_user(id) ON DELETE SET NULL,
    CONSTRAINT trace_link_org_id_key UNIQUE (org_id, id),
    -- A type goes with its project; the app role cannot delete one that has links (see link_type_removed).
    FOREIGN KEY (org_id, type_id) REFERENCES link_type (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, source_id) REFERENCES requirement (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, target_id) REFERENCES requirement (org_id, id) ON DELETE CASCADE,
    CONSTRAINT trace_link_not_self CHECK (source_id <> target_id)
);

CREATE UNIQUE INDEX trace_link_live_idx ON trace_link (type_id, source_id, target_id) WHERE deleted_at IS NULL;
CREATE INDEX trace_link_source_idx ON trace_link (org_id, source_id) WHERE deleted_at IS NULL;
CREATE INDEX trace_link_target_idx ON trace_link (org_id, target_id) WHERE deleted_at IS NULL;
CREATE INDEX trace_link_type_idx ON trace_link (org_id, type_id);

-- What a new link must be, whoever writes it: between two requirements out of the trash, of a type the
-- source's project may use and not retired, no repeat, within each end's share of links
-- (trace.MaxLinksPerRequirement), and for an acyclic type no way back from the target to the source within
-- trace.MaxCycleDepth links. It walks every link, not only those the writer sees, so it runs as the owner,
-- under a lock that keeps two links closing one cycle from passing together. It records the ends' revisions.
-- +goose StatementBegin
CREATE FUNCTION trace_link_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    t        link_type;
    src      requirement;
    tgt      requirement;
    way_back boolean;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('trace_link:' || NEW.org_id::text, 0));
    SELECT * INTO t FROM link_type WHERE org_id = NEW.org_id AND id = NEW.type_id;
    SELECT * INTO src FROM requirement WHERE org_id = NEW.org_id AND id = NEW.source_id;
    SELECT * INTO tgt FROM requirement WHERE org_id = NEW.org_id AND id = NEW.target_id;
    IF src.kind <> 'requirement' OR tgt.kind <> 'requirement' OR src.trashed_at IS NOT NULL OR tgt.trashed_at IS NOT NULL THEN
        RAISE EXCEPTION 'a link joins two requirements out of the trash'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_ends';
    END IF;
    IF t.retired OR (t.project_id IS NOT NULL AND t.project_id <> src.project_id) THEN
        RAISE EXCEPTION 'a link takes a live type of its source''s project or the organization'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_type';
    END IF;
    -- A type whose phrases read the same both ways is symmetric, so B to A repeats A to B.
    IF EXISTS (SELECT 1 FROM trace_link l
               WHERE l.org_id = NEW.org_id AND l.type_id = NEW.type_id AND l.deleted_at IS NULL
                 AND ((l.source_id = NEW.source_id AND l.target_id = NEW.target_id)
                      OR (t.forward = t.backward AND l.source_id = NEW.target_id AND l.target_id = NEW.source_id))) THEN
        RAISE EXCEPTION 'those requirements are already linked that way'
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'trace_link_repeat';
    END IF;
    IF (SELECT count(*) FROM trace_link l WHERE l.org_id = NEW.org_id AND l.deleted_at IS NULL
          AND (l.source_id = NEW.source_id OR l.target_id = NEW.source_id)) >= 500
       OR (SELECT count(*) FROM trace_link l WHERE l.org_id = NEW.org_id AND l.deleted_at IS NULL
          AND (l.source_id = NEW.target_id OR l.target_id = NEW.target_id)) >= 500 THEN
        RAISE EXCEPTION 'a requirement holds at most 500 links'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_limit';
    END IF;
    IF t.acyclic THEN
        WITH RECURSIVE downstream AS (
            SELECT l.target_id, ARRAY[l.target_id] AS path
            FROM trace_link l
            WHERE l.org_id = NEW.org_id AND l.type_id = NEW.type_id AND l.deleted_at IS NULL AND l.source_id = NEW.target_id
            UNION ALL
            SELECT l.target_id, d.path || l.target_id
            FROM trace_link l
            JOIN downstream d ON l.source_id = d.target_id
            WHERE l.org_id = NEW.org_id AND l.type_id = NEW.type_id AND l.deleted_at IS NULL
              AND array_length(d.path, 1) < 64 AND NOT l.target_id = ANY (d.path)
        )
        SELECT EXISTS (SELECT 1 FROM downstream WHERE target_id = NEW.source_id) INTO way_back;
        IF way_back THEN
            RAISE EXCEPTION 'a link of an acyclic type cannot come back around to its source'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_cycle';
        END IF;
    END IF;
    NEW.source_reviewed_rev := src.revision;
    NEW.target_reviewed_rev := tgt.revision;
    NEW.deleted_at := NULL;
    NEW.deleted_by := NULL;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER trace_link_guard BEFORE INSERT ON trace_link
    FOR EACH ROW EXECUTE FUNCTION trace_link_guard();

-- A link is removed once, and stays what it was: only the tombstone, and the reviewed revisions #36 moves on,
-- change.
-- +goose StatementBegin
CREATE FUNCTION trace_link_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.type_id <> OLD.type_id OR NEW.source_id <> OLD.source_id OR NEW.target_id <> OLD.target_id
       OR NEW.created_at <> OLD.created_at OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR (OLD.deleted_at IS NOT NULL AND NEW.deleted_at IS DISTINCT FROM OLD.deleted_at) THEN
        RAISE EXCEPTION 'a link keeps its ends and its type, and stays removed'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'trace_link_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER trace_link_fixed BEFORE UPDATE ON trace_link
    FOR EACH ROW EXECUTE FUNCTION trace_link_fixed();

-- A link made or removed is logged on both ends, read from each, whoever writes it; the far end may be one
-- the writer only views, which its own log would not take from them.
-- +goose StatementBegin
CREATE FUNCTION trace_link_logged() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    t          link_type;
    source_key text;
    target_key text;
    removed    boolean := TG_OP = 'UPDATE';
    actor      uuid := CASE WHEN TG_OP = 'UPDATE' THEN NEW.deleted_by ELSE NEW.created_by END;
BEGIN
    SELECT * INTO t FROM link_type WHERE org_id = NEW.org_id AND id = NEW.type_id;
    SELECT p.key || '-R' || r.number INTO source_key FROM requirement r JOIN project p ON p.id = r.project_id WHERE r.id = NEW.source_id;
    SELECT p.key || '-R' || r.number INTO target_key FROM requirement r JOIN project p ON p.id = r.project_id WHERE r.id = NEW.target_id;
    INSERT INTO requirement_change (org_id, requirement_id, field, from_value, to_value, actor_id) VALUES
        (NEW.org_id, NEW.source_id, 'link',
         CASE WHEN removed THEN t.forward || ' ' || target_key ELSE '' END,
         CASE WHEN removed THEN '' ELSE t.forward || ' ' || target_key END, actor),
        (NEW.org_id, NEW.target_id, 'link',
         CASE WHEN removed THEN t.backward || ' ' || source_key ELSE '' END,
         CASE WHEN removed THEN '' ELSE t.backward || ' ' || source_key END, actor);
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER trace_link_made AFTER INSERT ON trace_link
    FOR EACH ROW EXECUTE FUNCTION trace_link_logged();
CREATE TRIGGER trace_link_removed AFTER UPDATE OF deleted_at ON trace_link
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL) EXECUTE FUNCTION trace_link_logged();

-- How many links a type has, live or every one ever, whoever asks: what decides whether it may go is every
-- link, tombstones included, not what the asker sees.
-- +goose StatementBegin
CREATE FUNCTION link_type_usage(type uuid, live_only boolean) RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT count(*)::int FROM trace_link
    WHERE org_id = current_org_id() AND type_id = link_type_usage.type AND (NOT live_only OR deleted_at IS NULL)
$$;
-- +goose StatementEnd

-- Whether the actor shapes a link type: the organization's administrators its own, a project's
-- administrators the project's while it is open.
-- +goose StatementBegin
CREATE FUNCTION link_type_shaped(project uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN project IS NULL THEN current_actor_administers()
                ELSE perm_project_shaped(project, current_actor_id()) END
$$;
-- +goose StatementEnd

-- The six types every organization starts with; their keys are what imports and scripts name.
-- +goose StatementBegin
CREATE FUNCTION link_type_seed(org uuid) RETURNS void
    LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    INSERT INTO link_type (org_id, key, name, forward, backward, acyclic, suspect_end, counts) VALUES
        (org, 'derives', 'Derives', 'derives from', 'is derived into', true, 'target', 'decomposition'),
        (org, 'refines', 'Refines', 'refines', 'is refined by', true, 'target', 'decomposition'),
        (org, 'satisfies', 'Satisfies', 'satisfies', 'is satisfied by', true, 'target', 'decomposition'),
        (org, 'verifies', 'Verifies', 'verifies', 'is verified by', true, 'target', 'verification'),
        (org, 'conflicts', 'Conflicts', 'conflicts with', 'conflicts with', false, 'both', 'nothing'),
        (org, 'relates', 'Relates', 'relates to', 'relates to', false, 'none', 'nothing')
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION org_link_type_seed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM link_type_seed(NEW.id);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER org_link_type_seed AFTER INSERT ON org
    FOR EACH ROW EXECUTE FUNCTION org_link_type_seed();

SELECT link_type_seed(id) FROM org;

ALTER TABLE link_type         ENABLE ROW LEVEL SECURITY;
ALTER TABLE link_type         FORCE  ROW LEVEL SECURITY;
ALTER TABLE link_type_project ENABLE ROW LEVEL SECURITY;
ALTER TABLE link_type_project FORCE  ROW LEVEL SECURITY;
ALTER TABLE link_type_rule    ENABLE ROW LEVEL SECURITY;
ALTER TABLE link_type_rule    FORCE  ROW LEVEL SECURITY;
ALTER TABLE trace_link        ENABLE ROW LEVEL SECURITY;
ALTER TABLE trace_link        FORCE  ROW LEVEL SECURITY;

CREATE POLICY link_type_tenant_isolation ON link_type
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY link_type_project_tenant_isolation ON link_type_project
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY link_type_rule_tenant_isolation ON link_type_rule
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY trace_link_tenant_isolation ON trace_link
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY link_type_admin_bypass ON link_type TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY link_type_project_admin_bypass ON link_type_project TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY link_type_rule_admin_bypass ON link_type_rule TO exciter_admin USING (true) WITH CHECK (true);
CREATE POLICY trace_link_admin_bypass ON trace_link TO exciter_admin USING (true) WITH CHECK (true);

-- Through the app role every member reads the organization's types and whoever views a project its own; the
-- organization's administrators shape the first, the project's the rest, and nobody deletes a type with links.
CREATE POLICY link_type_visible ON link_type AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (project_id IS NULL OR perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY link_type_added ON link_type AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (link_type_shaped(project_id) AND created_by = current_actor_id());
CREATE POLICY link_type_changed ON link_type AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (link_type_shaped(project_id)) WITH CHECK (link_type_shaped(project_id));
CREATE POLICY link_type_removed ON link_type AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (link_type_shaped(project_id) AND link_type_usage(id, false) = 0);

CREATE POLICY link_type_project_visible ON link_type_project AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY link_type_project_added ON link_type_project AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY link_type_project_changed ON link_type_project AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id())) WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY link_type_project_removed ON link_type_project AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY link_type_rule_visible ON link_type_rule AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY link_type_rule_added ON link_type_rule AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_shaped(project_id, current_actor_id()));
CREATE POLICY link_type_rule_removed ON link_type_rule AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_shaped(project_id, current_actor_id()));

-- A link shows to whoever views both ends, is made in one's own name by whoever edits one end and views the
-- other, and is removed, never deleted, by whoever edits either end.
CREATE POLICY trace_link_visible ON trace_link AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(source_id, current_actor_id()) AND perm_requirement_viewable(target_id, current_actor_id()));
CREATE POLICY trace_link_added ON trace_link AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND (
        (perm_requirement_editable(source_id, current_actor_id()) AND perm_requirement_viewable(target_id, current_actor_id()))
        OR (perm_requirement_editable(target_id, current_actor_id()) AND perm_requirement_viewable(source_id, current_actor_id()))));
CREATE POLICY trace_link_removed ON trace_link AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (deleted_at IS NULL AND (perm_requirement_editable(source_id, current_actor_id()) OR perm_requirement_editable(target_id, current_actor_id())))
    WITH CHECK (deleted_by = current_actor_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON link_type, link_type_project, link_type_rule TO exciter_app, exciter_admin;
-- Links are tombstoned, not deleted, and keep their ends: the schema's default privileges would give the app
-- role everything.
GRANT SELECT, INSERT, UPDATE, DELETE ON trace_link TO exciter_admin;
REVOKE ALL ON trace_link FROM exciter_app;
GRANT SELECT, INSERT ON trace_link TO exciter_app;
GRANT UPDATE (deleted_at, deleted_by) ON trace_link TO exciter_app;

-- +goose Down
DROP TRIGGER IF EXISTS org_link_type_seed ON org;
DROP FUNCTION IF EXISTS org_link_type_seed();
DROP FUNCTION IF EXISTS link_type_seed(uuid);
DROP TABLE IF EXISTS trace_link;
DROP TABLE IF EXISTS link_type_rule;
DROP TABLE IF EXISTS link_type_project;
DROP TABLE IF EXISTS link_type;
DROP FUNCTION IF EXISTS trace_link_guard();
DROP FUNCTION IF EXISTS trace_link_fixed();
DROP FUNCTION IF EXISTS trace_link_logged();
DROP FUNCTION IF EXISTS link_type_usage(uuid, boolean);
DROP FUNCTION IF EXISTS link_type_shaped(uuid);
DROP FUNCTION IF EXISTS link_type_project_reach();
DROP FUNCTION IF EXISTS link_type_rule_reach();
DROP FUNCTION IF EXISTS link_type_names_free();
