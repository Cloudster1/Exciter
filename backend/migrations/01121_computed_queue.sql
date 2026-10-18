-- +goose Up
-- The queue of computed values to work out again takes marks without ever waiting (#248). A change used to upsert
-- several marks while the worker held the ones it was working on and upserted the next, so the two could wait on each
-- other until the database broke the change off. Marks are now only appended, with no unique or foreign key to wait
-- on; the worker takes a project's marks under its advisory lock, counts a requirement's marks once with the fewest
-- steps, and drops the marks of requirements and projects that are gone.

DROP TABLE computed_dirty;
CREATE TABLE computed_dirty (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    requirement_id uuid NOT NULL,
    project_id     uuid NOT NULL,
    steps          integer NOT NULL DEFAULT 0 CHECK (steps >= 0),
    since          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX computed_dirty_project_idx ON computed_dirty (project_id, requirement_id);

ALTER TABLE computed_dirty ENABLE ROW LEVEL SECURITY;
ALTER TABLE computed_dirty FORCE  ROW LEVEL SECURITY;
CREATE POLICY computed_dirty_installation ON computed_dirty USING (true) WITH CHECK (true);
GRANT SELECT, INSERT, UPDATE, DELETE ON computed_dirty TO exciter_admin;
REVOKE ALL ON computed_dirty FROM exciter_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION computed_mark(ids uuid[]) RETURNS void
    LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    INSERT INTO computed_dirty (requirement_id, project_id)
    SELECT r.id, r.project_id FROM requirement r WHERE r.id = ANY (ids) AND computed_in(r.project_id)
$$;

CREATE OR REPLACE FUNCTION computed_recompute(project uuid) RETURNS integer
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
    SELECT r.id, r.project_id FROM requirement r WHERE r.project_id = project AND r.trashed_at IS NULL;
    GET DIAGNOSTICS marked = ROW_COUNT;
    INSERT INTO attribute_recompute (project_id, total) VALUES (project, marked)
    ON CONFLICT (project_id) DO UPDATE SET total = EXCLUDED.total, started_at = now(), finished_at = NULL;
    RETURN marked;
END;
$$;

-- A requirement marked twice is one left to work out.
CREATE OR REPLACE FUNCTION computed_progress(project uuid) RETURNS TABLE (total integer, done integer, started_at timestamptz)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT a.total, GREATEST(0, a.total - (SELECT count(DISTINCT d.requirement_id)::int FROM computed_dirty d WHERE d.project_id = a.project_id)),
           a.started_at
    FROM attribute_recompute a
    WHERE a.project_id = computed_progress.project AND a.finished_at IS NULL
      AND perm_project_holds(a.project_id, current_actor_id(), 'view')
$$;
-- +goose StatementEnd

-- Marks waiting when the queue was made again are made again: every requirement of a project that computes.
INSERT INTO computed_dirty (requirement_id, project_id)
SELECT r.id, r.project_id FROM requirement r WHERE r.trashed_at IS NULL AND computed_in(r.project_id);

-- +goose Down
DROP TABLE computed_dirty;
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION computed_mark(ids uuid[]) RETURNS void
    LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    INSERT INTO computed_dirty (requirement_id, project_id)
    SELECT r.id, r.project_id FROM requirement r WHERE r.id = ANY (ids) AND computed_in(r.project_id)
    ON CONFLICT (requirement_id) DO UPDATE SET steps = 0
$$;

CREATE OR REPLACE FUNCTION computed_recompute(project uuid) RETURNS integer
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

CREATE OR REPLACE FUNCTION computed_progress(project uuid) RETURNS TABLE (total integer, done integer, started_at timestamptz)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT a.total, GREATEST(0, a.total - (SELECT count(*)::int FROM computed_dirty d WHERE d.project_id = a.project_id)), a.started_at
    FROM attribute_recompute a
    WHERE a.project_id = computed_progress.project AND a.finished_at IS NULL
      AND perm_project_holds(a.project_id, current_actor_id(), 'view')
$$;
-- +goose StatementEnd
