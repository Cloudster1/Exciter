-- +goose Up
-- Test results from CI (#200), as docs/api-contract-m9.md agrees: a run uploaded with its JUnit files is read by the
-- worker as its uploader, and each test case's outcome lands on the requirements it names, stamped with the revision
-- current at upload. A result past which the requirement was revised is outdated, as a suspect link is.

-- The branch whose evidence counts, per project; without a row it is testresult.DefaultCountingBranch.
CREATE TABLE evidence_settings (
    project_id      uuid PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    -- testresult.MaxNameLength.
    counting_branch text NOT NULL CHECK (btrim(counting_branch) <> '' AND char_length(counting_branch) <= 200),
    updated_by      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE test_run (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id   uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    -- testresult.MaxNameLength and MaxURLLength.
    repository   text NOT NULL CHECK (btrim(repository) <> '' AND char_length(repository) <= 200),
    commit_sha   text NOT NULL CHECK (commit_sha ~ '^[0-9a-f]{7,64}$'),
    branch       text NOT NULL CHECK (btrim(branch) <> '' AND char_length(branch) <= 200),
    job          text NOT NULL DEFAULT '' CHECK (char_length(job) <= 200),
    run_url      text NOT NULL DEFAULT '' CHECK (run_url = '' OR (run_url ~* '^https?://[^\s]+$' AND char_length(run_url) <= 2000)),
    state        text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'done', 'failed')),
    -- The cases as the upload read them, [{suite, name, outcome, message, durationMs, keys}], until the worker is done.
    cases        jsonb NOT NULL CHECK (jsonb_typeof(cases) = 'array'),
    cases_read   integer NOT NULL DEFAULT 0 CHECK (cases_read >= 0),
    matched      integer NOT NULL DEFAULT 0 CHECK (matched >= 0),
    passed       integer NOT NULL DEFAULT 0 CHECK (passed >= 0),
    failed       integer NOT NULL DEFAULT 0 CHECK (failed >= 0),
    skipped      integer NOT NULL DEFAULT 0 CHECK (skipped >= 0),
    errored      integer NOT NULL DEFAULT 0 CHECK (errored >= 0),
    -- The keys that verify nothing, [{key, test, problem}], at most testresult.MaxReported.
    reported     jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(reported) = 'array'),
    unreported   integer NOT NULL DEFAULT 0 CHECK (unreported >= 0),
    error        text NOT NULL DEFAULT '',
    uploaded_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    uploaded_at  timestamptz NOT NULL DEFAULT now(),
    started_at   timestamptz,
    finished_at  timestamptz,
    via_kind     text CHECK (via_kind IN ('token', 'oauth')),
    via_id       uuid,
    via_name     text,
    CONSTRAINT test_run_via_whole CHECK ((via_kind IS NULL) = (via_id IS NULL) AND (via_kind IS NULL) = (via_name IS NULL)),
    CONSTRAINT test_run_finished CHECK ((state IN ('done', 'failed')) = (finished_at IS NOT NULL))
);
CREATE INDEX test_run_branch_idx ON test_run (project_id, repository, branch, job, uploaded_at DESC);

-- One requirement's result of one test case in one run, never changed once written.
CREATE TABLE test_result (
    run_id         uuid NOT NULL REFERENCES test_run(id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    requirement_id uuid NOT NULL REFERENCES requirement(id) ON DELETE CASCADE,
    suite          text NOT NULL CHECK (char_length(suite) <= 1000),
    name           text NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 1000),
    outcome        text NOT NULL CHECK (outcome IN ('passed', 'failed', 'skipped', 'errored')),
    -- testresult.MaxMessageChars.
    message        text NOT NULL DEFAULT '' CHECK (char_length(message) <= 2000),
    duration_ms    integer NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    -- Stamped by the database: the newest revision made at or before the run's upload, 0 for none.
    revision       integer NOT NULL DEFAULT 0 CHECK (revision >= 0),
    PRIMARY KEY (run_id, requirement_id, suite, name)
);
CREATE INDEX test_result_requirement_idx ON test_result (requirement_id, run_id);

-- The latest finished run of each repository, branch and job: what counts as evidence. Only the database writes it,
-- as a run is done.
CREATE TABLE test_run_latest (
    project_id  uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    repository  text NOT NULL,
    branch      text NOT NULL,
    job         text NOT NULL,
    run_id      uuid NOT NULL REFERENCES test_run(id) ON DELETE CASCADE,
    uploaded_at timestamptz NOT NULL,
    PRIMARY KEY (project_id, repository, branch, job)
);
CREATE INDEX test_run_latest_run_idx ON test_run_latest (run_id);
CREATE INDEX test_run_latest_branch_idx ON test_run_latest (project_id, branch);

-- What counts for verified (#200): links as before, and the latest results on the counting branch.
ALTER TABLE coverage_rule ADD COLUMN verified_from text[] NOT NULL DEFAULT ARRAY['links']::text[],
    ADD CONSTRAINT coverage_rule_verified_from CHECK (cardinality(verified_from) >= 1 AND verified_from <@ ARRAY['links', 'tests']::text[]);

-- +goose StatementBegin
-- The branch a project's evidence counts on.
CREATE FUNCTION evidence_counting_branch(project uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE((SELECT s.counting_branch FROM evidence_settings s WHERE s.project_id = project), 'main')
$$;

-- The branch evidence is read on in this transaction: the one app.evidence_branch names, as a trace check does, or
-- the project's counting branch.
CREATE FUNCTION evidence_branch(project uuid) RETURNS text
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(NULLIF(current_setting('app.evidence_branch', true), ''), evidence_counting_branch(project))
$$;

-- A requirement's tests on the branch evidence is read on, as the reader sees them: how many, and how many passed
-- against its current revision.
CREATE FUNCTION requirement_test_counts(target uuid) RETURNS TABLE (tests integer, tests_whole integer, tests_outdated integer)
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    SELECT count(*)::int,
           count(*) FILTER (WHERE tr.outcome = 'passed' AND tr.revision >= r.revision)::int,
           count(*) FILTER (WHERE tr.revision < r.revision)::int
    FROM requirement r
    JOIN test_run_latest l ON l.project_id = r.project_id AND l.branch = evidence_branch(r.project_id)
    JOIN test_result tr ON tr.run_id = l.run_id AND tr.requirement_id = r.id
    WHERE r.id = target
$$;

-- A requirement's latest results summed up, worst first, for the query language's testResult.
CREATE FUNCTION requirement_test_result(target uuid) RETURNS text
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(
        (SELECT tr.outcome
         FROM requirement r
         JOIN test_run_latest l ON l.project_id = r.project_id AND l.branch = evidence_branch(r.project_id)
         JOIN test_result tr ON tr.run_id = l.run_id AND tr.requirement_id = r.id
         WHERE r.id = target
         ORDER BY array_position(ARRAY['errored', 'failed', 'skipped', 'passed'], tr.outcome)
         LIMIT 1), 'none')
$$;

-- What a run is stays what it was uploaded: the worker moves its state forward once, counts what it read and lets
-- go of the cases once done.
CREATE FUNCTION test_run_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.project_id, NEW.repository, NEW.commit_sha, NEW.branch, NEW.job, NEW.run_url, NEW.uploaded_by, NEW.uploaded_at)
       IS DISTINCT FROM (OLD.project_id, OLD.repository, OLD.commit_sha, OLD.branch, OLD.job, OLD.run_url, OLD.uploaded_by, OLD.uploaded_at) THEN
        RAISE EXCEPTION 'test_run_fixed' USING ERRCODE = 'check_violation', CONSTRAINT = 'test_run_fixed',
            DETAIL = 'A run stays what it was uploaded.';
    END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
        (OLD.state = 'queued' AND NEW.state IN ('running', 'failed')) OR
        (OLD.state = 'running' AND NEW.state IN ('done', 'failed'))) THEN
        RAISE EXCEPTION 'test_run_state' USING ERRCODE = 'check_violation', CONSTRAINT = 'test_run_state',
            DETAIL = 'A run moves from queued to running to done or failed, once.';
    END IF;
    IF NEW.cases IS DISTINCT FROM OLD.cases AND NEW.cases <> '[]'::jsonb THEN
        RAISE EXCEPTION 'test_run_cases' USING ERRCODE = 'check_violation', CONSTRAINT = 'test_run_cases',
            DETAIL = 'The cases a run read are let go of, never changed.';
    END IF;
    RETURN NEW;
END;
$$;

-- A finished run becomes the latest of its repository, branch and job unless a later upload finished first.
CREATE FUNCTION test_run_finished() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.state = 'done' AND OLD.state IS DISTINCT FROM 'done' THEN
        INSERT INTO test_run_latest (project_id, repository, branch, job, run_id, uploaded_at)
        VALUES (NEW.project_id, NEW.repository, NEW.branch, NEW.job, NEW.id, NEW.uploaded_at)
        ON CONFLICT (project_id, repository, branch, job) DO UPDATE SET run_id = EXCLUDED.run_id, uploaded_at = EXCLUDED.uploaded_at
        WHERE test_run_latest.uploaded_at <= EXCLUDED.uploaded_at;
    END IF;
    RETURN NULL;
END;
$$;

-- A result names the revision current when its run was uploaded, whatever the writer sent: a revision saved while
-- the worker read the run was not what the tests ran against.
CREATE FUNCTION test_result_stamped() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    SELECT COALESCE(max(v.number), 0) INTO NEW.revision
    FROM requirement_revision v, test_run t
    WHERE t.id = NEW.run_id AND v.requirement_id = NEW.requirement_id AND v.created_at <= t.uploaded_at;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER test_run_guard BEFORE UPDATE ON test_run FOR EACH ROW EXECUTE FUNCTION test_run_guard();
CREATE TRIGGER test_run_finished AFTER UPDATE OF state ON test_run FOR EACH ROW EXECUTE FUNCTION test_run_finished();
CREATE TRIGGER test_run_via BEFORE INSERT OR UPDATE ON test_run FOR EACH ROW EXECUTE FUNCTION via_stamped();
CREATE TRIGGER test_result_stamped BEFORE INSERT ON test_result FOR EACH ROW EXECUTE FUNCTION test_result_stamped();

ALTER TABLE evidence_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence_settings FORCE  ROW LEVEL SECURITY;
ALTER TABLE test_run          ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_run          FORCE  ROW LEVEL SECURITY;
ALTER TABLE test_result       ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_result       FORCE  ROW LEVEL SECURITY;
ALTER TABLE test_run_latest   ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_run_latest   FORCE  ROW LEVEL SECURITY;

CREATE POLICY evidence_settings_installation ON evidence_settings USING (true) WITH CHECK (true);
CREATE POLICY test_run_installation ON test_run USING (true) WITH CHECK (true);
CREATE POLICY test_result_installation ON test_result USING (true) WITH CHECK (true);
CREATE POLICY test_run_latest_installation ON test_run_latest USING (true) WITH CHECK (true);

-- Whoever views a project reads its counting branch; its administrators of an open project name it.
CREATE POLICY evidence_settings_readers ON evidence_settings AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY evidence_settings_keepers ON evidence_settings AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id)
                AND updated_by = current_actor_id());
CREATE POLICY evidence_settings_changers ON evidence_settings AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id))
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer') AND updated_by = current_actor_id());

-- Whoever views a project reads its runs and results, since a requirement shows them; an editor uploads a run in
-- their own name, and its uploader alone moves it on, which the worker does as them.
CREATE POLICY test_run_readers ON test_run AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY test_run_uploaders ON test_run AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'edit') AND uploaded_by = current_actor_id()
                AND state = 'queued' AND matched = 0 AND reported = '[]'::jsonb);
CREATE POLICY test_run_runners ON test_run AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (uploaded_by = current_actor_id()) WITH CHECK (uploaded_by = current_actor_id());

CREATE POLICY test_result_readers ON test_result AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
-- A result is written by its run's uploader while the run runs, for a requirement of the run's own project.
CREATE POLICY test_result_writers ON test_result AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM test_run t WHERE t.id = run_id AND t.project_id = test_result.project_id
                        AND t.state = 'running' AND t.uploaded_by = current_actor_id())
                AND EXISTS (SELECT 1 FROM requirement q WHERE q.id = requirement_id AND q.project_id = test_result.project_id
                            AND q.kind = 'requirement' AND q.trashed_at IS NULL));

CREATE POLICY test_run_latest_readers ON test_run_latest AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));

GRANT SELECT, INSERT, UPDATE, DELETE ON evidence_settings, test_run, test_result, test_run_latest TO exciter_admin;
REVOKE ALL ON evidence_settings, test_run, test_result, test_run_latest FROM exciter_app;
GRANT SELECT, INSERT ON evidence_settings, test_run, test_result TO exciter_app;
GRANT UPDATE (counting_branch, updated_by, updated_at) ON evidence_settings TO exciter_app;
GRANT UPDATE (state, cases, cases_read, matched, passed, failed, skipped, errored, reported, unreported, error, started_at, finished_at)
    ON test_run TO exciter_app;
GRANT SELECT ON test_run_latest TO exciter_app;
GRANT UPDATE (verified_from) ON coverage_rule TO exciter_app;
REVOKE ALL ON FUNCTION evidence_counting_branch(uuid), test_run_finished(), test_result_stamped() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION evidence_counting_branch(uuid) TO exciter_app, exciter_admin;

-- +goose StatementBegin
-- Verified counts the sources a rule lists (#200): links as before, and tests, all passed against the current
-- revision to be whole. A source that holds nothing does not stop another from making it full.
CREATE OR REPLACE FUNCTION requirement_coverage(target uuid, dimension text) RETURNS text
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    WITH req AS (
        SELECT r.id, r.kind, r.revision,
               CASE dimension
                   WHEN 'decomposed' THEN COALESCE(own.decomposed, every.decomposed, false)
                   WHEN 'implemented' THEN COALESCE(own.implemented, every.implemented, false)
                   WHEN 'verified' THEN COALESCE(own.verified, every.verified, false)
               END AS asks,
               COALESCE(own.verified_from, every.verified_from, ARRAY['links']::text[]) AS verified_from
        FROM requirement r
        LEFT JOIN coverage_rule own ON own.project_id = r.project_id AND own.type_id = r.type_id
        LEFT JOIN coverage_rule every ON every.project_id = r.project_id AND every.type_id IS NULL
        WHERE r.id = target
    ), t AS (
        SELECT count(*) FILTER (WHERE lt.counts = 'decomposition') AS decomposing,
               count(*) FILTER (WHERE lt.counts = 'decomposition' AND v.suspect) AS decomposing_suspect,
               count(*) FILTER (WHERE lt.counts = 'verification') AS verifying,
               count(*) FILTER (WHERE lt.counts = 'verification' AND v.suspect) AS verifying_suspect
        FROM trace_link_state v
        JOIN link_type lt ON lt.id = v.type_id
        JOIN requirement s ON s.id = v.source_id
        -- What waits in the trash covers nothing.
        WHERE v.target_id = target AND s.trashed_at IS NULL AND lt.counts <> 'nothing'
    ), a AS (
        SELECT count(*) FILTER (WHERE l.relation = 'implements') AS implementing,
               count(*) FILTER (WHERE l.relation = 'implements' AND l.done) AS implementing_done,
               count(*) FILTER (WHERE l.relation = 'implements' AND l.suspect) AS implementing_suspect,
               count(*) FILTER (WHERE l.relation = 'verifies') AS verifying,
               count(*) FILTER (WHERE l.relation = 'verifies' AND l.done) AS verifying_done,
               count(*) FILTER (WHERE l.relation = 'verifies' AND l.suspect) AS verifying_suspect
        -- A category Armature has not told since the issue moved is not taken for done.
        FROM (SELECT al.relation, al.status_category = 'done' AND NOT al.status_stale AS done,
                     req.revision > al.reviewed_rev AS suspect
              FROM armature_issue_link al, req
              WHERE al.requirement_id = target AND al.broken_at IS NULL AND al.detached_at IS NULL) l
    ), e AS (
        SELECT c.tests, c.tests_whole FROM requirement_test_counts(target) c
    ), v AS (
        SELECT 'links' = ANY (req.verified_from) AND t.verifying + a.verifying > 0 AS links_hold,
               t.verifying_suspect + a.verifying_suspect = 0 AND a.verifying_done = a.verifying AS links_whole,
               'tests' = ANY (req.verified_from) AND e.tests > 0 AS tests_hold,
               e.tests_whole = e.tests AS tests_whole
        FROM req, t, a, e
    )
    SELECT CASE
        WHEN req.kind <> 'requirement' OR NOT req.asks THEN 'na'
        WHEN dimension = 'decomposed' THEN
            CASE WHEN t.decomposing = 0 THEN 'none' WHEN t.decomposing_suspect = 0 THEN 'full' ELSE 'partial' END
        WHEN dimension = 'implemented' THEN
            CASE WHEN a.implementing = 0 THEN 'none'
                 WHEN a.implementing_done > 0 AND a.implementing_suspect = 0 THEN 'full' ELSE 'partial' END
        ELSE
            CASE WHEN NOT (v.links_hold OR v.tests_hold) THEN 'none'
                 WHEN (NOT v.links_hold OR v.links_whole) AND (NOT v.tests_hold OR v.tests_whole) THEN 'full'
                 ELSE 'partial' END
    END
    FROM req, t, a, v
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION requirement_coverage(target uuid, dimension text) RETURNS text
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    WITH req AS (
        SELECT r.id, r.kind, r.revision,
               CASE dimension
                   WHEN 'decomposed' THEN COALESCE(own.decomposed, every.decomposed, false)
                   WHEN 'implemented' THEN COALESCE(own.implemented, every.implemented, false)
                   WHEN 'verified' THEN COALESCE(own.verified, every.verified, false)
               END AS asks
        FROM requirement r
        LEFT JOIN coverage_rule own ON own.project_id = r.project_id AND own.type_id = r.type_id
        LEFT JOIN coverage_rule every ON every.project_id = r.project_id AND every.type_id IS NULL
        WHERE r.id = target
    ), t AS (
        SELECT count(*) FILTER (WHERE lt.counts = 'decomposition') AS decomposing,
               count(*) FILTER (WHERE lt.counts = 'decomposition' AND v.suspect) AS decomposing_suspect,
               count(*) FILTER (WHERE lt.counts = 'verification') AS verifying,
               count(*) FILTER (WHERE lt.counts = 'verification' AND v.suspect) AS verifying_suspect
        FROM trace_link_state v
        JOIN link_type lt ON lt.id = v.type_id
        JOIN requirement s ON s.id = v.source_id
        WHERE v.target_id = target AND s.trashed_at IS NULL AND lt.counts <> 'nothing'
    ), a AS (
        SELECT count(*) FILTER (WHERE l.relation = 'implements') AS implementing,
               count(*) FILTER (WHERE l.relation = 'implements' AND l.done) AS implementing_done,
               count(*) FILTER (WHERE l.relation = 'implements' AND l.suspect) AS implementing_suspect,
               count(*) FILTER (WHERE l.relation = 'verifies') AS verifying,
               count(*) FILTER (WHERE l.relation = 'verifies' AND l.done) AS verifying_done,
               count(*) FILTER (WHERE l.relation = 'verifies' AND l.suspect) AS verifying_suspect
        FROM (SELECT al.relation, al.status_category = 'done' AND NOT al.status_stale AS done,
                     req.revision > al.reviewed_rev AS suspect
              FROM armature_issue_link al, req
              WHERE al.requirement_id = target AND al.broken_at IS NULL AND al.detached_at IS NULL) l
    )
    SELECT CASE
        WHEN req.kind <> 'requirement' OR NOT req.asks THEN 'na'
        WHEN dimension = 'decomposed' THEN
            CASE WHEN t.decomposing = 0 THEN 'none' WHEN t.decomposing_suspect = 0 THEN 'full' ELSE 'partial' END
        WHEN dimension = 'implemented' THEN
            CASE WHEN a.implementing = 0 THEN 'none'
                 WHEN a.implementing_done > 0 AND a.implementing_suspect = 0 THEN 'full' ELSE 'partial' END
        ELSE
            CASE WHEN t.verifying + a.verifying = 0 THEN 'none'
                 WHEN t.verifying_suspect + a.verifying_suspect = 0 AND a.verifying_done = a.verifying THEN 'full'
                 ELSE 'partial' END
    END
    FROM req, t, a
$$;
-- +goose StatementEnd
ALTER TABLE coverage_rule DROP CONSTRAINT IF EXISTS coverage_rule_verified_from, DROP COLUMN IF EXISTS verified_from;
DROP TABLE IF EXISTS test_run_latest;
DROP TABLE IF EXISTS test_result;
DROP TABLE IF EXISTS test_run;
DROP TABLE IF EXISTS evidence_settings;
DROP FUNCTION IF EXISTS requirement_test_result(uuid);
DROP FUNCTION IF EXISTS requirement_test_counts(uuid);
DROP FUNCTION IF EXISTS evidence_branch(uuid);
DROP FUNCTION IF EXISTS evidence_counting_branch(uuid);
DROP FUNCTION IF EXISTS test_result_stamped();
DROP FUNCTION IF EXISTS test_run_finished();
DROP FUNCTION IF EXISTS test_run_guard();
