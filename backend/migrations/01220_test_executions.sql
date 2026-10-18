-- +goose Up
-- Test plans and test executions (#251), as docs/api-contract-m12.md agrees: a plan groups test cases, an execution
-- runs them against a baseline, a version and a configuration, pinned as it starts, and its testers record each step.

CREATE TABLE test_plan (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id  uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    -- testexec.MaxTitleLength and MaxDescriptionLength.
    title       text NOT NULL CHECK (btrim(title) <> '' AND char_length(title) <= 100),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    scope       text NOT NULL CHECK (scope IN ('cases', 'query')),
    query       text NOT NULL DEFAULT '',
    created_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT test_plan_query CHECK ((scope = 'query') = (btrim(query) <> ''))
);
CREATE INDEX test_plan_project_idx ON test_plan (project_id, lower(title));

CREATE TABLE test_plan_case (
    plan_id        uuid NOT NULL REFERENCES test_plan(id) ON DELETE CASCADE,
    requirement_id uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    position       integer NOT NULL CHECK (position >= 0),
    PRIMARY KEY (plan_id, requirement_id)
);

CREATE TABLE test_execution (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id     uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    plan_id        uuid REFERENCES test_plan(id) ON DELETE SET NULL,
    plan_title     text NOT NULL DEFAULT '',
    title          text NOT NULL CHECK (btrim(title) <> '' AND char_length(title) <= 100),
    baseline_id    uuid REFERENCES baseline(id) ON DELETE SET NULL,
    baseline_name  text NOT NULL DEFAULT '',
    -- testexec.MaxVersionLength and MaxConfigurationLength.
    version        text NOT NULL DEFAULT '' CHECK (char_length(version) <= 200),
    configuration  text NOT NULL DEFAULT '' CHECK (char_length(configuration) <= 500),
    manager_id     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    due_at         timestamptz,
    state          text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'closed', 'aborted')),
    -- What the plan held that the execution left out, [{key, reason}].
    excluded       jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(excluded) = 'array'),
    close_comment  text NOT NULL DEFAULT '' CHECK (char_length(close_comment) <= 500),
    reminded_at    timestamptz,
    opened_at      timestamptz NOT NULL DEFAULT now(),
    closed_at      timestamptz,
    closed_by      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    CONSTRAINT test_execution_closed CHECK ((state = 'open') = (closed_at IS NULL))
);
CREATE INDEX test_execution_project_idx ON test_execution (project_id, opened_at DESC);
CREATE INDEX test_execution_due_idx ON test_execution (due_at) WHERE state = 'open' AND reminded_at IS NULL;

CREATE TABLE test_execution_tester (
    execution_id uuid NOT NULL REFERENCES test_execution(id) ON DELETE CASCADE,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
    user_id      uuid REFERENCES app_user(id) ON DELETE CASCADE,
    group_id     uuid REFERENCES groups(id) ON DELETE CASCADE,
    CONSTRAINT test_execution_tester_whole CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
        (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL))
);
CREATE UNIQUE INDEX test_execution_tester_idx ON test_execution_tester (execution_id, subject_type, user_id, group_id) NULLS NOT DISTINCT;

-- One iteration of a case, pinned at the revision it runs, with what was recorded of it.
CREATE TABLE test_execution_case (
    execution_id   uuid NOT NULL REFERENCES test_execution(id) ON DELETE CASCADE,
    requirement_id uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    iteration      integer NOT NULL CHECK (iteration >= 1),
    position       integer NOT NULL CHECK (position >= 0),
    revision       integer NOT NULL CHECK (revision >= 1),
    -- The parameters' values of the iteration, by name, as the pinned revision gives them.
    "values"       jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof("values") = 'object'),
    outcome        text CHECK (outcome IN ('passed', 'failed', 'blocked', 'skipped')),
    -- testexec.MaxNoteLength.
    note           text NOT NULL DEFAULT '' CHECK (char_length(note) <= 4000),
    source         text CHECK (source IN ('tester', 'ci')),
    recorded_by    uuid REFERENCES app_user(id) ON DELETE SET NULL,
    recorded_at    timestamptz,
    ci_run_id      uuid REFERENCES test_run(id) ON DELETE SET NULL,
    ci_suite       text NOT NULL DEFAULT '',
    ci_name        text NOT NULL DEFAULT '',
    ci_commit      text NOT NULL DEFAULT '',
    ci_uploaded_at timestamptz,
    PRIMARY KEY (execution_id, requirement_id, iteration),
    CONSTRAINT test_execution_case_recorded CHECK ((outcome IS NULL) = (source IS NULL) AND (outcome IS NULL) = (recorded_at IS NULL))
);
CREATE INDEX test_execution_case_last_idx ON test_execution_case (requirement_id, recorded_at DESC) WHERE outcome IS NOT NULL;

CREATE TABLE test_execution_step (
    execution_id   uuid NOT NULL,
    requirement_id uuid NOT NULL,
    iteration      integer NOT NULL,
    step           integer NOT NULL CHECK (step BETWEEN 1 AND 100),
    outcome        text NOT NULL CHECK (outcome IN ('passed', 'failed', 'blocked', 'skipped')),
    note           text NOT NULL DEFAULT '' CHECK (char_length(note) <= 4000),
    recorded_by    uuid REFERENCES app_user(id) ON DELETE SET NULL,
    recorded_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (execution_id, requirement_id, iteration, step),
    FOREIGN KEY (execution_id, requirement_id, iteration) REFERENCES test_execution_case ON DELETE CASCADE
);

CREATE TABLE test_execution_file (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    execution_id   uuid NOT NULL,
    requirement_id uuid NOT NULL,
    iteration      integer NOT NULL,
    step           integer CHECK (step BETWEEN 1 AND 100),
    file_name      text NOT NULL CHECK (btrim(file_name) <> ''),
    content_type   text NOT NULL,
    size           bigint NOT NULL CHECK (size > 0),
    sha256         text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    object_key     text NOT NULL DEFAULT '',
    uploaded_by    uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (execution_id, requirement_id, iteration) REFERENCES test_execution_case ON DELETE CASCADE
);
CREATE INDEX test_execution_file_case_idx ON test_execution_file (execution_id, requirement_id, iteration);

CREATE TABLE test_execution_issue (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    execution_id   uuid NOT NULL,
    requirement_id uuid NOT NULL,
    iteration      integer NOT NULL,
    step           integer CHECK (step BETWEEN 1 AND 100),
    issue_key      text NOT NULL CHECK (btrim(issue_key) <> ''),
    created_by     uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (execution_id, requirement_id, iteration) REFERENCES test_execution_case ON DELETE CASCADE
);

-- An execution's notifications name it rather than a requirement.
ALTER TABLE notification ADD COLUMN execution_id uuid REFERENCES test_execution(id) ON DELETE CASCADE,
    DROP CONSTRAINT notification_about_something,
    ADD CONSTRAINT notification_about_something
        CHECK (requirement_id IS NOT NULL OR session_id IS NOT NULL OR code_scan_id IS NOT NULL OR execution_id IS NOT NULL),
    DROP CONSTRAINT notification_kind_check,
    ADD CONSTRAINT notification_kind_check CHECK (kind IN ('mentioned', 'review_requested', 'reviewed', 'suspect', 'replied',
        'commented', 'resolved', 'revised', 'transitioned', 'source_revised', 'session_invited', 'session_round',
        'session_reminder', 'session_closed', 'code_outdated', 'code_orphaned', 'term_changed',
        'execution_assigned', 'execution_reminder', 'execution_closed'));

-- +goose StatementBegin
-- Whether the actor manages an execution: whoever started it, or one of its project's administrators.
CREATE FUNCTION test_execution_manages(eid uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM test_execution e
        WHERE e.id = eid AND (e.manager_id = actor OR perm_project_holds(e.project_id, actor, 'administer')))
$$;

-- Whether the actor tests an execution: named in person or through a group, and able to comment in its project.
CREATE FUNCTION test_execution_tests(eid uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM test_execution e JOIN test_execution_tester t ON t.execution_id = e.id
        WHERE e.id = eid AND perm_project_holds(e.project_id, actor, 'comment')
          AND (t.user_id = actor OR EXISTS (SELECT 1 FROM group_member m WHERE m.group_id = t.group_id AND m.user_id = actor)))
$$;

-- Whether the actor records in an execution now: it is open, its project too, and they manage or test it.
CREATE FUNCTION test_execution_records(eid uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM test_execution e WHERE e.id = eid AND e.state = 'open' AND project_open(e.project_id))
       AND (test_execution_manages(eid, actor) OR test_execution_tests(eid, actor))
$$;

-- Pins the cases an execution runs, in order, once, as its manager starts it: each a live test case of its project,
-- at the revision its baseline holds or the current one, once per iteration its parameters line up; answers how
-- many iterations it pinned.
CREATE FUNCTION test_execution_start(eid uuid, ids uuid[]) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    e test_execution%ROWTYPE;
    rid uuid;
    pos integer := 0;
    rev integer;
    params jsonb;
    runs integer;
    n integer;
    pinned integer := 0;
BEGIN
    SELECT * INTO e FROM test_execution WHERE id = eid FOR UPDATE;
    IF e.id IS NULL OR e.state <> 'open' OR e.opened_at <> now() OR e.manager_id IS DISTINCT FROM current_actor_id() THEN
        RAISE EXCEPTION 'only the manager starting an execution pins its cases'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'test_execution_start';
    END IF;
    FOREACH rid IN ARRAY ids LOOP
        IF NOT EXISTS (SELECT 1 FROM requirement r JOIN requirement_type t ON t.id = r.type_id
                       WHERE r.id = rid AND r.project_id = e.project_id AND r.kind = 'requirement' AND r.trashed_at IS NULL
                         AND t.role = 'testCase') THEN
            RAISE EXCEPTION 'an execution runs live test cases of its own project'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'test_execution_start';
        END IF;
        IF e.baseline_id IS NOT NULL THEN
            SELECT i.revision INTO rev FROM baseline_item i WHERE i.baseline_id = e.baseline_id AND i.requirement_id = rid;
            IF rev IS NULL THEN
                RAISE EXCEPTION 'the baseline does not hold the case'
                    USING ERRCODE = 'check_violation', CONSTRAINT = 'test_execution_start';
            END IF;
        ELSE
            SELECT r.revision INTO rev FROM requirement r WHERE r.id = rid;
        END IF;
        SELECT v.parameters INTO params FROM requirement_revision v WHERE v.requirement_id = rid AND v.number = rev;
        runs := GREATEST(1, COALESCE(jsonb_array_length(params -> 0 -> 'values'), 1));
        FOR n IN 1..runs LOOP
            INSERT INTO test_execution_case (execution_id, requirement_id, iteration, position, revision, "values")
            VALUES (eid, rid, n, pos, rev, COALESCE((
                SELECT jsonb_object_agg(p ->> 'name', p -> 'values' ->> (n - 1)) FROM jsonb_array_elements(params) p), '{}'::jsonb));
            pinned := pinned + 1;
        END LOOP;
        pos := pos + 1;
    END LOOP;
    RETURN pinned;
END;
$$;

-- An execution keeps its project, plan, baseline, manager and what it left out; it moves from open to closed or
-- aborted once, and then changes no more.
CREATE FUNCTION test_execution_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.project_id <> OLD.project_id OR NEW.opened_at <> OLD.opened_at OR NEW.excluded IS DISTINCT FROM OLD.excluded
       OR (NEW.plan_id IS NOT NULL AND NEW.plan_id IS DISTINCT FROM OLD.plan_id)
       OR (NEW.baseline_id IS NOT NULL AND NEW.baseline_id IS DISTINCT FROM OLD.baseline_id)
       OR (NEW.manager_id IS NOT NULL AND NEW.manager_id IS DISTINCT FROM OLD.manager_id)
       OR (NEW.plan_title, NEW.baseline_name) IS DISTINCT FROM (OLD.plan_title, OLD.baseline_name) THEN
        RAISE EXCEPTION 'an execution keeps what it was started over' USING ERRCODE = 'check_violation', CONSTRAINT = 'test_execution_fixed';
    END IF;
    IF OLD.state <> 'open' AND (NEW.state, NEW.title, NEW.version, NEW.configuration, NEW.due_at, NEW.close_comment, NEW.closed_at)
       IS DISTINCT FROM (OLD.state, OLD.title, OLD.version, OLD.configuration, OLD.due_at, OLD.close_comment, OLD.closed_at) THEN
        RAISE EXCEPTION 'a closed execution does not change' USING ERRCODE = 'check_violation', CONSTRAINT = 'test_execution_closed';
    END IF;
    RETURN NEW;
END;
$$;

-- What an execution holds is written while it is open, however it is written; a person forgotten or a CI run reaped
-- later only lets go of who or what it names.
CREATE FUNCTION test_execution_open() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    let_go text[] := ARRAY['recorded_by', 'uploaded_by', 'created_by', 'ci_run_id'];
BEGIN
    IF TG_OP = 'UPDATE' AND (to_jsonb(NEW) - let_go) = (to_jsonb(OLD) - let_go) THEN
        RETURN NEW;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM test_execution e WHERE e.id = NEW.execution_id AND e.state = 'open') THEN
        RAISE EXCEPTION 'a closed execution does not change' USING ERRCODE = 'check_violation', CONSTRAINT = 'test_execution_closed';
    END IF;
    RETURN NEW;
END;
$$;

-- A plan's cases are live test cases of its project.
CREATE FUNCTION test_plan_case_kept() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM test_plan p JOIN requirement r ON r.project_id = p.project_id JOIN requirement_type t ON t.id = r.type_id
                   WHERE p.id = NEW.plan_id AND r.id = NEW.requirement_id AND r.kind = 'requirement' AND r.trashed_at IS NULL
                     AND t.role = 'testCase') THEN
        RAISE EXCEPTION 'a plan holds live test cases of its own project'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'test_plan_case_kept';
    END IF;
    RETURN NEW;
END;
$$;

-- A file's object is named after the execution and the file, under the installation's prefix, whatever an insert says.
CREATE FUNCTION test_execution_file_key() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.object_key := 'org/' || (SELECT id FROM installation) || '/execution/' || NEW.execution_id || '/' || NEW.id;
    RETURN NEW;
END;
$$;

-- A file deleted, by its uploader or with what holds it, leaves its object for the worker's reaper.
CREATE FUNCTION test_execution_file_tombstone() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    INSERT INTO attachment_tombstone (object_key) VALUES (OLD.object_key) ON CONFLICT DO NOTHING;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER test_execution_guard BEFORE UPDATE ON test_execution FOR EACH ROW EXECUTE FUNCTION test_execution_guard();
CREATE TRIGGER test_execution_case_open BEFORE INSERT OR UPDATE ON test_execution_case FOR EACH ROW EXECUTE FUNCTION test_execution_open();
CREATE TRIGGER test_execution_step_open BEFORE INSERT OR UPDATE ON test_execution_step FOR EACH ROW EXECUTE FUNCTION test_execution_open();
CREATE TRIGGER test_execution_file_open BEFORE INSERT OR UPDATE ON test_execution_file FOR EACH ROW EXECUTE FUNCTION test_execution_open();
CREATE TRIGGER test_execution_issue_open BEFORE INSERT OR UPDATE ON test_execution_issue FOR EACH ROW EXECUTE FUNCTION test_execution_open();
CREATE TRIGGER test_plan_case_kept BEFORE INSERT OR UPDATE ON test_plan_case FOR EACH ROW EXECUTE FUNCTION test_plan_case_kept();
CREATE TRIGGER test_execution_file_key BEFORE INSERT ON test_execution_file FOR EACH ROW EXECUTE FUNCTION test_execution_file_key();
CREATE TRIGGER test_execution_file_tombstone AFTER DELETE ON test_execution_file FOR EACH ROW EXECUTE FUNCTION test_execution_file_tombstone();

ALTER TABLE test_plan             ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_plan             FORCE  ROW LEVEL SECURITY;
ALTER TABLE test_plan_case        ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_plan_case        FORCE  ROW LEVEL SECURITY;
ALTER TABLE test_execution        ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_execution        FORCE  ROW LEVEL SECURITY;
ALTER TABLE test_execution_tester ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_execution_tester FORCE  ROW LEVEL SECURITY;
ALTER TABLE test_execution_case   ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_execution_case   FORCE  ROW LEVEL SECURITY;
ALTER TABLE test_execution_step   ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_execution_step   FORCE  ROW LEVEL SECURITY;
ALTER TABLE test_execution_file   ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_execution_file   FORCE  ROW LEVEL SECURITY;
ALTER TABLE test_execution_issue  ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_execution_issue  FORCE  ROW LEVEL SECURITY;

CREATE POLICY test_plan_installation ON test_plan USING (true) WITH CHECK (true);
CREATE POLICY test_plan_case_installation ON test_plan_case USING (true) WITH CHECK (true);
CREATE POLICY test_execution_installation ON test_execution USING (true) WITH CHECK (true);
CREATE POLICY test_execution_tester_installation ON test_execution_tester USING (true) WITH CHECK (true);
CREATE POLICY test_execution_case_installation ON test_execution_case USING (true) WITH CHECK (true);
CREATE POLICY test_execution_step_installation ON test_execution_step USING (true) WITH CHECK (true);
CREATE POLICY test_execution_file_installation ON test_execution_file USING (true) WITH CHECK (true);
CREATE POLICY test_execution_issue_installation ON test_execution_issue USING (true) WITH CHECK (true);

-- A project's readers read its plans; its editors make and change them while it is open.
CREATE POLICY test_plan_readers ON test_plan AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY test_plan_makers ON test_plan AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND perm_project_holds(project_id, current_actor_id(), 'edit') AND project_open(project_id));
CREATE POLICY test_plan_changers ON test_plan AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'edit') AND project_open(project_id))
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'edit') AND project_open(project_id));
CREATE POLICY test_plan_removers ON test_plan AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'edit') AND project_open(project_id));
CREATE POLICY test_plan_case_readers ON test_plan_case AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM test_plan p WHERE p.id = plan_id));
CREATE POLICY test_plan_case_writers ON test_plan_case AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM test_plan p WHERE p.id = plan_id AND perm_project_holds(p.project_id, current_actor_id(), 'edit')
                        AND project_open(p.project_id)));
CREATE POLICY test_plan_case_removers ON test_plan_case AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (EXISTS (SELECT 1 FROM test_plan p WHERE p.id = plan_id AND perm_project_holds(p.project_id, current_actor_id(), 'edit')
                   AND project_open(p.project_id)));

-- A project's readers read its executions; its editors start them as their manager; the manager changes one.
CREATE POLICY test_execution_readers ON test_execution AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY test_execution_starters ON test_execution AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (manager_id = current_actor_id() AND state = 'open' AND perm_project_holds(project_id, current_actor_id(), 'edit')
                AND project_open(project_id));
CREATE POLICY test_execution_managers ON test_execution AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (test_execution_manages(id, current_actor_id())) WITH CHECK (test_execution_manages(id, current_actor_id()));
-- Testers are named as the execution starts, by its manager.
CREATE POLICY test_execution_tester_readers ON test_execution_tester AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM test_execution e WHERE e.id = execution_id));
CREATE POLICY test_execution_tester_namers ON test_execution_tester AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM test_execution e WHERE e.id = execution_id AND e.opened_at = now() AND e.manager_id = current_actor_id()));
-- Cases are pinned by test_execution_start alone; their results, steps, files and issues are written by whoever
-- records in the execution, as themselves.
CREATE POLICY test_execution_case_readers ON test_execution_case AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM test_execution e WHERE e.id = execution_id));
CREATE POLICY test_execution_case_recorders ON test_execution_case AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (test_execution_records(execution_id, current_actor_id()))
    WITH CHECK (test_execution_records(execution_id, current_actor_id())
                AND (outcome IS NULL OR (source = 'tester' AND recorded_by = current_actor_id())));
CREATE POLICY test_execution_step_readers ON test_execution_step AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM test_execution e WHERE e.id = execution_id));
CREATE POLICY test_execution_step_recorders ON test_execution_step AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (recorded_by = current_actor_id() AND test_execution_records(execution_id, current_actor_id()));
CREATE POLICY test_execution_step_changers ON test_execution_step AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (test_execution_records(execution_id, current_actor_id()))
    WITH CHECK (recorded_by = current_actor_id() AND test_execution_records(execution_id, current_actor_id()));
CREATE POLICY test_execution_step_removers ON test_execution_step AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (test_execution_records(execution_id, current_actor_id()));
CREATE POLICY test_execution_file_readers ON test_execution_file AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM test_execution e WHERE e.id = execution_id));
CREATE POLICY test_execution_file_uploaders ON test_execution_file AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (uploaded_by = current_actor_id() AND test_execution_records(execution_id, current_actor_id()));
CREATE POLICY test_execution_file_removers ON test_execution_file AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (uploaded_by = current_actor_id() AND EXISTS (SELECT 1 FROM test_execution e WHERE e.id = execution_id AND e.state = 'open'));
CREATE POLICY test_execution_issue_readers ON test_execution_issue AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM test_execution e WHERE e.id = execution_id));
CREATE POLICY test_execution_issue_filers ON test_execution_issue AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (created_by = current_actor_id() AND test_execution_records(execution_id, current_actor_id()));

-- A notification about an execution is for whoever may read its project.
DROP POLICY notification_own_read ON notification;
DROP POLICY notification_own_write ON notification;
CREATE POLICY notification_own_read ON notification AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (user_id = current_actor_id() AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id())
         AND (term_id IS NULL OR EXISTS (SELECT 1 FROM glossary_term t WHERE t.id = term_id)))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM code_scan c WHERE c.id = code_scan_id
                                               AND perm_project_holds(c.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM test_execution e WHERE e.id = execution_id
                                               AND perm_project_holds(e.project_id, current_actor_id(), 'view')))));
CREATE POLICY notification_own_write ON notification AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (user_id = current_actor_id() AND read_at IS NULL AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id())
         AND (term_id IS NULL OR EXISTS (SELECT 1 FROM glossary_term t WHERE t.id = term_id)))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM code_scan c WHERE c.id = code_scan_id
                                               AND perm_project_holds(c.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM test_execution e WHERE e.id = execution_id
                                               AND perm_project_holds(e.project_id, current_actor_id(), 'view')))));

GRANT SELECT, INSERT, UPDATE, DELETE ON test_plan, test_plan_case, test_execution, test_execution_tester, test_execution_case,
    test_execution_step, test_execution_file, test_execution_issue TO exciter_admin;
REVOKE ALL ON test_plan, test_plan_case, test_execution, test_execution_tester, test_execution_case,
    test_execution_step, test_execution_file, test_execution_issue FROM exciter_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON test_plan TO exciter_app;
GRANT SELECT, INSERT, DELETE ON test_plan_case TO exciter_app;
GRANT SELECT, INSERT ON test_execution, test_execution_tester, test_execution_issue TO exciter_app;
GRANT UPDATE (title, version, configuration, due_at, state, close_comment, reminded_at, closed_at, closed_by) ON test_execution TO exciter_app;
GRANT SELECT ON test_execution_case TO exciter_app;
GRANT UPDATE (outcome, note, source, recorded_by, recorded_at) ON test_execution_case TO exciter_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON test_execution_step TO exciter_app;
GRANT SELECT, INSERT, DELETE ON test_execution_file TO exciter_app;

-- +goose Down
DROP POLICY IF EXISTS notification_own_read ON notification;
DROP POLICY IF EXISTS notification_own_write ON notification;
DELETE FROM notification WHERE execution_id IS NOT NULL OR kind LIKE 'execution_%';
CREATE POLICY notification_own_read ON notification AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (user_id = current_actor_id() AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id())
         AND (term_id IS NULL OR EXISTS (SELECT 1 FROM glossary_term t WHERE t.id = term_id)))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM code_scan c WHERE c.id = code_scan_id
                                               AND perm_project_holds(c.project_id, current_actor_id(), 'view')))));
CREATE POLICY notification_own_write ON notification AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (user_id = current_actor_id() AND read_at IS NULL AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id())
         AND (term_id IS NULL OR EXISTS (SELECT 1 FROM glossary_term t WHERE t.id = term_id)))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM code_scan c WHERE c.id = code_scan_id
                                               AND perm_project_holds(c.project_id, current_actor_id(), 'view')))));
ALTER TABLE notification DROP CONSTRAINT notification_kind_check,
    ADD CONSTRAINT notification_kind_check CHECK (kind IN ('mentioned', 'review_requested', 'reviewed', 'suspect', 'replied',
        'commented', 'resolved', 'revised', 'transitioned', 'source_revised', 'session_invited', 'session_round',
        'session_reminder', 'session_closed', 'code_outdated', 'code_orphaned', 'term_changed')),
    DROP CONSTRAINT notification_about_something,
    ADD CONSTRAINT notification_about_something CHECK (requirement_id IS NOT NULL OR session_id IS NOT NULL OR code_scan_id IS NOT NULL),
    DROP COLUMN execution_id;
DROP TABLE test_execution_issue;
DROP TABLE test_execution_file;
DROP TABLE test_execution_step;
DROP TABLE test_execution_case;
DROP TABLE test_execution_tester;
DROP TABLE test_execution;
DROP TABLE test_plan_case;
DROP TABLE test_plan;
DROP FUNCTION test_execution_file_tombstone();
DROP FUNCTION test_execution_file_key();
DROP FUNCTION test_plan_case_kept();
DROP FUNCTION test_execution_open();
DROP FUNCTION test_execution_guard();
DROP FUNCTION test_execution_start(uuid, uuid[]);
DROP FUNCTION test_execution_records(uuid, uuid);
DROP FUNCTION test_execution_tests(uuid, uuid);
DROP FUNCTION test_execution_manages(uuid, uuid);
