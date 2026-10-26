-- +goose Up
-- A workflow names only statuses of its own project (#340). Each step carries its workflow's project, which the
-- keys below tie to the workflow and to both statuses it names, so neither the step nor a status or workflow moved
-- to another project can break the rule. The service already refuses such a graph; this holds every role to it.
ALTER TABLE workflow_step ADD COLUMN project_id uuid;

-- No service path wrote a step naming another project's status; one written by hand cannot keep its name.
UPDATE workflow_step s SET on_revision_status_id = NULL
FROM workflow w, req_status r
WHERE w.id = s.workflow_id AND r.id = s.on_revision_status_id AND r.project_id <> w.project_id;
DELETE FROM workflow_step s USING workflow w, req_status r
WHERE w.id = s.workflow_id AND r.id = s.status_id AND r.project_id <> w.project_id;

UPDATE workflow_step s SET project_id = w.project_id FROM workflow w WHERE w.id = s.workflow_id;
ALTER TABLE workflow_step ALTER COLUMN project_id SET NOT NULL;

-- +goose StatementBegin
-- The project is the workflow's, whatever an insert says.
CREATE FUNCTION workflow_step_project() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.project_id := (SELECT project_id FROM workflow WHERE id = NEW.workflow_id);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION workflow_step_project() FROM PUBLIC;

CREATE TRIGGER workflow_step_project BEFORE INSERT OR UPDATE OF workflow_id ON workflow_step
    FOR EACH ROW EXECUTE FUNCTION workflow_step_project();

ALTER TABLE workflow ADD CONSTRAINT workflow_project_id_id_key UNIQUE (project_id, id);
ALTER TABLE req_status ADD CONSTRAINT req_status_project_id_id_key UNIQUE (project_id, id);

ALTER TABLE workflow_step DROP CONSTRAINT workflow_step_workflow_id_fkey;
ALTER TABLE workflow_step DROP CONSTRAINT workflow_step_status_id_fkey;
ALTER TABLE workflow_step DROP CONSTRAINT workflow_step_on_revision_status_id_fkey;
ALTER TABLE workflow_step ADD CONSTRAINT workflow_step_workflow_of_project
    FOREIGN KEY (project_id, workflow_id) REFERENCES workflow (project_id, id) ON DELETE CASCADE ON UPDATE CASCADE;
-- Checked at commit, so a project's cascade may take its statuses before the steps that name them.
ALTER TABLE workflow_step ADD CONSTRAINT workflow_step_status_of_project
    FOREIGN KEY (project_id, status_id) REFERENCES req_status (project_id, id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE workflow_step ADD CONSTRAINT workflow_step_on_revision_status_of_project
    FOREIGN KEY (project_id, on_revision_status_id) REFERENCES req_status (project_id, id) DEFERRABLE INITIALLY DEFERRED;

-- +goose Down
ALTER TABLE workflow_step DROP CONSTRAINT workflow_step_on_revision_status_of_project;
ALTER TABLE workflow_step DROP CONSTRAINT workflow_step_status_of_project;
ALTER TABLE workflow_step DROP CONSTRAINT workflow_step_workflow_of_project;
ALTER TABLE workflow_step ADD CONSTRAINT workflow_step_workflow_id_fkey
    FOREIGN KEY (workflow_id) REFERENCES workflow(id) ON DELETE CASCADE;
ALTER TABLE workflow_step ADD CONSTRAINT workflow_step_status_id_fkey
    FOREIGN KEY (status_id) REFERENCES req_status(id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE workflow_step ADD CONSTRAINT workflow_step_on_revision_status_id_fkey
    FOREIGN KEY (on_revision_status_id) REFERENCES req_status(id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE req_status DROP CONSTRAINT req_status_project_id_id_key;
ALTER TABLE workflow DROP CONSTRAINT workflow_project_id_id_key;
DROP TRIGGER workflow_step_project ON workflow_step;
DROP FUNCTION workflow_step_project();
ALTER TABLE workflow_step DROP COLUMN project_id;
