-- +goose Up
-- Computed attributes (#213), as docs/api-contract-m11.md agrees: an attribute may be worked out from a formula,
-- kept with attributes and link types named by id. Its values are kept beside the requirement, never in its
-- attributes, its revisions or what a signature covers; the worker alone writes them, and the database marks what to
-- work out again in the transaction of every change a formula reads. A baseline records the values it saw.

ALTER TABLE attribute_def
    ADD COLUMN formula     text,
    ADD COLUMN on_sections boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT attribute_def_formula_kind CHECK (formula IS NULL OR kind IN ('number', 'text', 'boolean', 'date')),
    ADD CONSTRAINT attribute_def_formula_length CHECK (formula IS NULL OR char_length(formula) BETWEEN 1 AND 8000),
    ADD CONSTRAINT attribute_def_formula_plain CHECK (formula IS NULL OR (NOT required AND required_from IS NULL
        AND depends_on IS NULL AND choices = '{}'::jsonb AND editable_by = '[]'::jsonb)),
    ADD CONSTRAINT attribute_def_sections_computed CHECK (formula IS NOT NULL OR NOT on_sections);

-- +goose StatementBegin
-- An attribute is made computed or typed and stays so; its formula may change.
CREATE FUNCTION attribute_def_formula_fixed() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.formula IS NULL) <> (OLD.formula IS NULL) THEN
        RAISE EXCEPTION 'an attribute stays computed or typed as it was made'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'attribute_def_formula_fixed';
    END IF;
    RETURN NEW;
END;
$$;

-- Whether a project has computed attributes, so a change elsewhere costs nothing.
CREATE FUNCTION computed_in(project uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM attribute_def WHERE project_id = computed_in.project AND formula IS NOT NULL)
$$;

-- A computed attribute takes no typed value.
CREATE FUNCTION requirement_computed_untyped() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    typed text;
BEGIN
    SELECT a.name INTO typed FROM attribute_def a
    WHERE a.project_id = NEW.project_id AND a.formula IS NOT NULL AND NEW.attributes ? a.id::text LIMIT 1;
    IF typed IS NOT NULL THEN
        RAISE EXCEPTION 'attribute % is computed and takes no value', typed
            USING ERRCODE = 'check_violation', CONSTRAINT = 'requirement_computed_untyped';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER attribute_def_formula_fixed BEFORE UPDATE OF formula ON attribute_def
    FOR EACH ROW EXECUTE FUNCTION attribute_def_formula_fixed();
CREATE TRIGGER requirement_computed_untyped BEFORE INSERT OR UPDATE OF attributes, project_id ON requirement
    FOR EACH ROW EXECUTE FUNCTION requirement_computed_untyped();

-- A requirement's computed values by attribute id, and why one could not be had; the worker's alone to write.
CREATE TABLE requirement_computed (
    requirement_id uuid PRIMARY KEY REFERENCES requirement(id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    "values"       jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof("values") = 'object'),
    errors         jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(errors) = 'object'),
    computed_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX requirement_computed_project_idx ON requirement_computed (project_id);

ALTER TABLE requirement_computed ENABLE ROW LEVEL SECURITY;
ALTER TABLE requirement_computed FORCE  ROW LEVEL SECURITY;
CREATE POLICY requirement_computed_installation ON requirement_computed USING (true) WITH CHECK (true);
CREATE POLICY requirement_computed_readers ON requirement_computed AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_requirement_viewable(requirement_id, current_actor_id()));
GRANT SELECT, INSERT, UPDATE, DELETE ON requirement_computed TO exciter_admin;
REVOKE ALL ON requirement_computed FROM exciter_app;
GRANT SELECT ON requirement_computed TO exciter_app;

-- The requirements to work out again, marked by the change that touched what they read. steps counts how often a
-- value changing marked it in a row, which stops a cycle older links make.
CREATE TABLE computed_dirty (
    requirement_id uuid PRIMARY KEY REFERENCES requirement(id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    steps          integer NOT NULL DEFAULT 0 CHECK (steps >= 0),
    since          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX computed_dirty_project_idx ON computed_dirty (project_id, since);

ALTER TABLE computed_dirty ENABLE ROW LEVEL SECURITY;
ALTER TABLE computed_dirty FORCE  ROW LEVEL SECURITY;
CREATE POLICY computed_dirty_installation ON computed_dirty USING (true) WITH CHECK (true);
GRANT SELECT, INSERT, UPDATE, DELETE ON computed_dirty TO exciter_admin;
REVOKE ALL ON computed_dirty FROM exciter_app;

-- A project's recompute after a formula changed: how many it marked, and since and until when.
CREATE TABLE attribute_recompute (
    project_id  uuid PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    total       integer NOT NULL CHECK (total >= 0),
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);

ALTER TABLE attribute_recompute ENABLE ROW LEVEL SECURITY;
ALTER TABLE attribute_recompute FORCE  ROW LEVEL SECURITY;
CREATE POLICY attribute_recompute_installation ON attribute_recompute USING (true) WITH CHECK (true);
CREATE POLICY attribute_recompute_readers ON attribute_recompute AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
GRANT SELECT, INSERT, UPDATE, DELETE ON attribute_recompute TO exciter_admin;
REVOKE ALL ON attribute_recompute FROM exciter_app;
GRANT SELECT ON attribute_recompute TO exciter_app;

-- +goose StatementBegin
-- A requirement's computed values, as the reader sees them; empty when it has none.
CREATE FUNCTION requirement_computed_of(requirement uuid) RETURNS jsonb
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    SELECT COALESCE((SELECT c."values" FROM requirement_computed c WHERE c.requirement_id = requirement_computed_of.requirement), '{}'::jsonb)
$$;

-- Marks requirements to work out again, in projects with computed attributes; a mark by a change starts over.
CREATE FUNCTION computed_mark(ids uuid[]) RETURNS void
    LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    INSERT INTO computed_dirty (requirement_id, project_id)
    SELECT r.id, r.project_id FROM requirement r WHERE r.id = ANY (ids) AND computed_in(r.project_id)
    ON CONFLICT (requirement_id) DO UPDATE SET steps = 0
$$;

-- Marks every requirement of a project to work out again, when a formula changed; its administrators' to ask.
CREATE FUNCTION computed_recompute(project uuid) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    marked integer;
BEGIN
    IF NOT perm_project_shaped(project, current_actor_id()) THEN
        RAISE EXCEPTION 'only the project''s administrators recompute its attributes'
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NOT computed_in(project) THEN
        DELETE FROM requirement_computed WHERE project_id = project;
        DELETE FROM computed_dirty WHERE project_id = project;
        DELETE FROM attribute_recompute WHERE project_id = project;
        RETURN 0;
    END IF;
    INSERT INTO computed_dirty (requirement_id, project_id)
    SELECT r.id, r.project_id FROM requirement r WHERE r.project_id = project AND r.trashed_at IS NULL
    ON CONFLICT (requirement_id) DO UPDATE SET steps = 0;
    GET DIAGNOSTICS marked = ROW_COUNT;
    INSERT INTO attribute_recompute (project_id, total) VALUES (project, marked)
    ON CONFLICT (project_id) DO UPDATE SET total = EXCLUDED.total, started_at = now(), finished_at = NULL;
    RETURN marked;
END;
$$;

-- How far a project's recompute has got, for a reader of the project: what it marked, and what is left of that.
CREATE FUNCTION computed_progress(project uuid) RETURNS TABLE (total integer, done integer, started_at timestamptz)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT a.total, GREATEST(0, a.total - (SELECT count(*)::int FROM computed_dirty d WHERE d.project_id = a.project_id)), a.started_at
    FROM attribute_recompute a
    WHERE a.project_id = computed_progress.project AND a.finished_at IS NULL
      AND perm_project_holds(a.project_id, current_actor_id(), 'view')
$$;

-- Whether a project has values waiting to be worked out again, for a baseline to say so.
CREATE FUNCTION computed_pending(project uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM computed_dirty d WHERE d.project_id = computed_pending.project)
        AND perm_project_holds(computed_pending.project, current_actor_id(), 'view')
$$;

-- A requirement changed: it, and the parents it left and joined, read again.
CREATE FUNCTION computed_requirement_changed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM computed_mark(ARRAY[OLD.parent_id]);
        RETURN OLD;
    END IF;
    IF TG_OP = 'INSERT' THEN
        PERFORM computed_mark(ARRAY[NEW.id, NEW.parent_id]);
    ELSIF NEW.project_id <> OLD.project_id THEN
        -- Values worked out by another project's attributes mean nothing here.
        DELETE FROM requirement_computed WHERE requirement_id = NEW.id;
        PERFORM computed_mark(ARRAY[NEW.id, NEW.parent_id, OLD.parent_id]);
    ELSE
        PERFORM computed_mark(ARRAY[NEW.id, NEW.parent_id, OLD.parent_id]);
    END IF;
    RETURN NEW;
END;
$$;

-- A link made or removed: both its ends read again.
CREATE FUNCTION computed_link_changed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM computed_mark(ARRAY[NEW.source_id, NEW.target_id]);
    RETURN NEW;
END;
$$;

-- A test result landed, or a finding was accepted or taken back: its requirement reads again.
CREATE FUNCTION computed_requirement_touched() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM computed_mark(ARRAY[OLD.requirement_id]);
        RETURN OLD;
    END IF;
    PERFORM computed_mark(ARRAY[NEW.requirement_id]);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER computed_requirement_inserted AFTER INSERT OR DELETE ON requirement
    FOR EACH ROW EXECUTE FUNCTION computed_requirement_changed();
CREATE TRIGGER computed_requirement_updated
    AFTER UPDATE OF title, status_id, owner_id, priority, attributes, type_id, parent_id, project_id, trashed_at, revision, kind
    ON requirement FOR EACH ROW EXECUTE FUNCTION computed_requirement_changed();
CREATE TRIGGER computed_link_changed AFTER INSERT OR UPDATE OF deleted_at ON trace_link
    FOR EACH ROW EXECUTE FUNCTION computed_link_changed();
CREATE TRIGGER computed_test_result AFTER INSERT ON test_result
    FOR EACH ROW EXECUTE FUNCTION computed_requirement_touched();
CREATE TRIGGER computed_quality_acceptance AFTER INSERT OR DELETE ON quality_acceptance
    FOR EACH ROW EXECUTE FUNCTION computed_requirement_touched();

REVOKE ALL ON FUNCTION computed_mark(uuid[]), computed_in(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION computed_mark(uuid[]), computed_in(uuid) TO exciter_admin;
REVOKE ALL ON FUNCTION computed_recompute(uuid), computed_progress(uuid), computed_pending(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION computed_recompute(uuid), computed_progress(uuid), computed_pending(uuid) TO exciter_app, exciter_admin;

-- A baseline records each computed value it saw, by its attribute's name and kind then, and whether a recompute
-- was still running.
ALTER TABLE baseline ADD COLUMN computed_stale boolean NOT NULL DEFAULT false;

CREATE TABLE baseline_computed (
    baseline_id    uuid NOT NULL REFERENCES baseline(id) ON DELETE CASCADE,
    requirement_id uuid NOT NULL,
    attribute_id   uuid NOT NULL,
    name           text NOT NULL,
    kind           text NOT NULL CHECK (kind IN ('number', 'text', 'boolean', 'date')),
    value          jsonb NOT NULL,
    PRIMARY KEY (baseline_id, requirement_id, attribute_id)
);

CREATE TRIGGER baseline_computed_frozen BEFORE INSERT OR UPDATE OR DELETE ON baseline_computed
    FOR EACH ROW EXECUTE FUNCTION baseline_rows_frozen();

ALTER TABLE baseline_computed ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline_computed FORCE  ROW LEVEL SECURITY;
CREATE POLICY baseline_computed_installation ON baseline_computed USING (true) WITH CHECK (true);
CREATE POLICY baseline_computed_readers ON baseline_computed AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id));
CREATE POLICY baseline_computed_takers ON baseline_computed AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id AND b.created_by = current_actor_id()));
GRANT SELECT, INSERT, UPDATE, DELETE ON baseline_computed TO exciter_admin;
REVOKE ALL ON baseline_computed FROM exciter_app;
GRANT SELECT, INSERT ON baseline_computed TO exciter_app;

-- +goose StatementBegin
-- A computed value as the hash writes it after its requirement's id.
CREATE FUNCTION baseline_computed_line(c baseline_computed) RETURNS text
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT format('computed %s %s %s', c.attribute_id, c.kind, c.value::text)
$$;

CREATE OR REPLACE FUNCTION baseline_content_hash(bid uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT encode(sha256(convert_to(
        (SELECT format(E'baseline %s %s %s\n', b.id, b.project_id, COALESCE(b.root_id::text, '-')) FROM baseline b WHERE b.id = bid)
        || COALESCE((SELECT string_agg(format(E'item %s %s %s %s\n', i.requirement_id, i.revision, COALESCE(v.hash, '-'),
                                              array_to_string(i.path, '.')), '' ORDER BY i.path, i.requirement_id)
                     FROM baseline_item i
                     LEFT JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
                     WHERE i.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'link %s %s %s\n', l.type_id, l.source_id, l.target_id), ''
                                       ORDER BY l.type_id, l.source_id, l.target_id)
                     FROM baseline_link l WHERE l.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'external %s\n', e.link_id), '' ORDER BY e.link_id)
                     FROM baseline_external_link e WHERE e.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'page %s\n', g.link_id), '' ORDER BY g.link_id)
                     FROM baseline_page_link g WHERE g.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'evidence %s %s\n', x.requirement_id, baseline_evidence_line(x)), ''
                                       ORDER BY x.requirement_id, baseline_evidence_line(x) COLLATE "C")
                     FROM baseline_evidence x WHERE x.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'%s %s\n', c.requirement_id, baseline_computed_line(c)), ''
                                       ORDER BY c.requirement_id, baseline_computed_line(c) COLLATE "C")
                     FROM baseline_computed c WHERE c.baseline_id = bid), ''),
        'UTF8')), 'hex')
$$;

CREATE OR REPLACE FUNCTION baseline_content_rows(bid uuid)
    RETURNS TABLE (kind text, a uuid, b uuid, c uuid, revision integer, hash text, path integer[])
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH readable AS (
        SELECT 1 FROM baseline x WHERE x.id = bid AND perm_project_holds(x.project_id, current_actor_id(), 'view'))
    SELECT 'item', i.requirement_id, NULL::uuid, NULL::uuid, i.revision, v.hash, i.path
    FROM baseline_item i LEFT JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
    WHERE i.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'link', l.type_id, l.source_id, l.target_id, NULL, NULL, NULL
    FROM baseline_link l WHERE l.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'external', e.link_id, NULL, NULL, NULL, NULL, NULL
    FROM baseline_external_link e WHERE e.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'page', g.link_id, NULL, NULL, NULL, NULL, NULL
    FROM baseline_page_link g WHERE g.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'evidence', x.requirement_id, NULL, NULL, x.revision, baseline_evidence_line(x), NULL
    FROM baseline_evidence x WHERE x.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'computed', c.requirement_id, NULL, NULL, NULL, baseline_computed_line(c), NULL
    FROM baseline_computed c WHERE c.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION baseline_content_hash(bid uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT encode(sha256(convert_to(
        (SELECT format(E'baseline %s %s %s\n', b.id, b.project_id, COALESCE(b.root_id::text, '-')) FROM baseline b WHERE b.id = bid)
        || COALESCE((SELECT string_agg(format(E'item %s %s %s %s\n', i.requirement_id, i.revision, COALESCE(v.hash, '-'),
                                              array_to_string(i.path, '.')), '' ORDER BY i.path, i.requirement_id)
                     FROM baseline_item i
                     LEFT JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
                     WHERE i.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'link %s %s %s\n', l.type_id, l.source_id, l.target_id), ''
                                       ORDER BY l.type_id, l.source_id, l.target_id)
                     FROM baseline_link l WHERE l.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'external %s\n', e.link_id), '' ORDER BY e.link_id)
                     FROM baseline_external_link e WHERE e.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'page %s\n', g.link_id), '' ORDER BY g.link_id)
                     FROM baseline_page_link g WHERE g.baseline_id = bid), '')
        || COALESCE((SELECT string_agg(format(E'evidence %s %s\n', x.requirement_id, baseline_evidence_line(x)), ''
                                       ORDER BY x.requirement_id, baseline_evidence_line(x) COLLATE "C")
                     FROM baseline_evidence x WHERE x.baseline_id = bid), ''),
        'UTF8')), 'hex')
$$;

CREATE OR REPLACE FUNCTION baseline_content_rows(bid uuid)
    RETURNS TABLE (kind text, a uuid, b uuid, c uuid, revision integer, hash text, path integer[])
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH readable AS (
        SELECT 1 FROM baseline x WHERE x.id = bid AND perm_project_holds(x.project_id, current_actor_id(), 'view'))
    SELECT 'item', i.requirement_id, NULL::uuid, NULL::uuid, i.revision, v.hash, i.path
    FROM baseline_item i LEFT JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
    WHERE i.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'link', l.type_id, l.source_id, l.target_id, NULL, NULL, NULL
    FROM baseline_link l WHERE l.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'external', e.link_id, NULL, NULL, NULL, NULL, NULL
    FROM baseline_external_link e WHERE e.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'page', g.link_id, NULL, NULL, NULL, NULL, NULL
    FROM baseline_page_link g WHERE g.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
    UNION ALL
    SELECT 'evidence', x.requirement_id, NULL, NULL, x.revision, baseline_evidence_line(x), NULL
    FROM baseline_evidence x WHERE x.baseline_id = bid AND EXISTS (SELECT 1 FROM readable)
$$;
-- +goose StatementEnd
DROP FUNCTION baseline_computed_line(baseline_computed);
DROP TABLE baseline_computed;
ALTER TABLE baseline DROP COLUMN computed_stale;
DROP TRIGGER computed_quality_acceptance ON quality_acceptance;
DROP TRIGGER computed_test_result ON test_result;
DROP TRIGGER computed_link_changed ON trace_link;
DROP TRIGGER computed_requirement_updated ON requirement;
DROP TRIGGER computed_requirement_inserted ON requirement;
DROP TRIGGER requirement_computed_untyped ON requirement;
DROP TRIGGER attribute_def_formula_fixed ON attribute_def;
DROP FUNCTION computed_requirement_touched();
DROP FUNCTION computed_link_changed();
DROP FUNCTION computed_requirement_changed();
DROP FUNCTION computed_pending(uuid);
DROP FUNCTION computed_progress(uuid);
DROP FUNCTION computed_recompute(uuid);
DROP FUNCTION computed_mark(uuid[]);
DROP FUNCTION requirement_computed_of(uuid);
DROP TABLE attribute_recompute;
DROP TABLE computed_dirty;
DROP TABLE requirement_computed;
DROP FUNCTION requirement_computed_untyped();
DROP FUNCTION attribute_def_formula_fixed();
DELETE FROM attribute_def WHERE formula IS NOT NULL;
ALTER TABLE attribute_def DROP CONSTRAINT attribute_def_sections_computed, DROP CONSTRAINT attribute_def_formula_plain,
    DROP CONSTRAINT attribute_def_formula_length, DROP CONSTRAINT attribute_def_formula_kind,
    DROP COLUMN on_sections, DROP COLUMN formula;
DROP FUNCTION computed_in(uuid);
