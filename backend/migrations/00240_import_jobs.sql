-- +goose Up
-- A CSV file on its way into a project (#23): the file as it was read, how its columns, words and people are
-- read, and how far the worker has come. The worker runs it as the person who started it, so whatever it
-- writes is held to that person's rights like any other write.
CREATE TABLE import_job (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id  uuid NOT NULL,
    created_by  uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    file_name   text NOT NULL,
    -- The header row, and every other row as {line, cells}; nothing is written to the project until it runs.
    headers     jsonb NOT NULL,
    rows        jsonb NOT NULL,
    state       text NOT NULL DEFAULT 'uploaded'
        CHECK (state IN ('uploaded', 'queued', 'running', 'done', 'failed', 'cancelled')),
    mapping     jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- The words and people answered for: {values, people}.
    choices     jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- What the mapping makes of the file, kept so a read does not read the project again.
    preview     jsonb NOT NULL DEFAULT '{}'::jsonb,
    done        integer NOT NULL DEFAULT 0,
    total       integer NOT NULL,
    imported    text[] NOT NULL DEFAULT '{}',
    updated     text[] NOT NULL DEFAULT '{}',
    refused     jsonb NOT NULL DEFAULT '[]'::jsonb,
    error       text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz,
    CONSTRAINT import_job_file_name CHECK (btrim(file_name) <> ''),
    CONSTRAINT import_job_rows CHECK (jsonb_typeof(rows) = 'array' AND jsonb_array_length(rows) <= 5000 AND total = jsonb_array_length(rows)),
    CONSTRAINT import_job_progress CHECK (done BETWEEN 0 AND total),
    FOREIGN KEY (org_id, project_id) REFERENCES project (org_id, id) ON DELETE CASCADE
);

CREATE INDEX import_job_project_idx ON import_job (org_id, project_id, created_at DESC);

-- The file never changes once uploaded, the mapping only until the import is queued, and an import moves only
-- forward: uploaded, queued, running, then done, failed or cancelled.
-- +goose StatementBegin
CREATE FUNCTION import_job_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.org_id, NEW.project_id, NEW.created_by, NEW.file_name, NEW.headers, NEW.rows, NEW.total, NEW.created_at)
       IS DISTINCT FROM (OLD.org_id, OLD.project_id, OLD.created_by, OLD.file_name, OLD.headers, OLD.rows, OLD.total, OLD.created_at) THEN
        RAISE EXCEPTION 'import_job_file_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The file of an import never changes; upload it again instead.';
    END IF;
    IF OLD.state <> 'uploaded' AND (NEW.mapping, NEW.choices) IS DISTINCT FROM (OLD.mapping, OLD.choices) THEN
        RAISE EXCEPTION 'import_job_mapping_fixed' USING ERRCODE = 'check_violation',
            DETAIL = 'The mapping of an import that started never changes.';
    END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
           (OLD.state = 'uploaded' AND NEW.state IN ('queued', 'cancelled'))
        OR (OLD.state = 'queued' AND NEW.state IN ('running', 'cancelled'))
        OR (OLD.state = 'running' AND NEW.state IN ('done', 'failed', 'cancelled'))) THEN
        RAISE EXCEPTION 'import_job_forward' USING ERRCODE = 'check_violation',
            DETAIL = 'An import moves from uploaded to queued to running, and then ends.';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER import_job_guard BEFORE UPDATE ON import_job
    FOR EACH ROW EXECUTE FUNCTION import_job_guard();

ALTER TABLE import_job ENABLE ROW LEVEL SECURITY;
ALTER TABLE import_job FORCE  ROW LEVEL SECURITY;

CREATE POLICY import_job_tenant_isolation ON import_job
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY import_job_admin_bypass ON import_job TO exciter_admin USING (true) WITH CHECK (true);

-- An import is made by who administers the project, in their own name; it is its maker's, and the project's
-- administrators', to read and to stop, and its maker's alone to map and start, which the worker does as them.
CREATE POLICY import_job_seen ON import_job AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY import_job_made_by_administrators ON import_job AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND state = 'uploaded' AND done = 0
                AND perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY import_job_changed ON import_job AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'))
    WITH CHECK (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'));
CREATE POLICY import_job_removed ON import_job AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (created_by = current_actor_id() OR perm_project_holds(project_id, current_actor_id(), 'administer'));

GRANT SELECT, INSERT, UPDATE, DELETE ON import_job TO exciter_app, exciter_admin;

-- An import sets the status of a requirement it has just made, which no other write may: the status moves by
-- transitions alone. Only in the transaction that made it, as somebody who administers its project, and only
-- to a status of its workflow.
-- +goose StatementBegin
CREATE FUNCTION requirement_import_status(requirement uuid, status uuid) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    r requirement%ROWTYPE;
BEGIN
    SELECT * INTO r FROM requirement WHERE id = requirement_import_status.requirement AND org_id = current_org_id() FOR UPDATE;
    IF r.id IS NULL OR r.kind <> 'requirement' OR r.created_at <> now() OR r.revision <> 1
       OR r.created_by IS DISTINCT FROM current_actor_id()
       OR NOT perm_project_holds(r.project_id, current_actor_id(), 'administer') THEN
        RAISE EXCEPTION 'requirement_import_status' USING ERRCODE = 'insufficient_privilege',
            DETAIL = 'An import sets the status of a requirement it has just made, and of no other.';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM workflow_step WHERE workflow_id = requirement_workflow(r.type_id, r.project_id)
                   AND status_id = requirement_import_status.status) THEN
        RAISE EXCEPTION 'requirement_import_status_unknown' USING ERRCODE = 'check_violation',
            DETAIL = 'The status is not one of the requirement''s workflow.';
    END IF;
    UPDATE requirement SET status_id = requirement_import_status.status WHERE id = r.id;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS requirement_import_status(uuid, uuid);
DROP TABLE IF EXISTS import_job;
DROP FUNCTION IF EXISTS import_job_guard();
