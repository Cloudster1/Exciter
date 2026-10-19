-- +goose Up
-- Test executions count as evidence (#252): a CI upload fills the open executions holding the test cases it
-- verifies, a coverage rule may count test cases by their last results, and a baseline records those results.

ALTER TABLE coverage_rule DROP CONSTRAINT coverage_rule_verified_from,
    ADD CONSTRAINT coverage_rule_verified_from CHECK (cardinality(verified_from) >= 1
        AND verified_from <@ ARRAY['links', 'tests', 'code', 'runs']::text[]);

-- A test case's last result a baseline recorded names its execution, which may be gone since.
ALTER TABLE baseline_evidence DROP CONSTRAINT baseline_evidence_kind_check,
    ADD CONSTRAINT baseline_evidence_kind_check CHECK (kind IN ('test', 'code', 'run')),
    ADD COLUMN execution_id uuid;

-- +goose StatementBegin
-- A test case's last result, as the reader sees it: its outcome in the execution, not aborted, that recorded it last,
-- the worst over its iterations with one not run counting as none, and the revision that execution pinned.
CREATE FUNCTION test_case_last_run(target uuid) RETURNS TABLE (outcome text, execution_id uuid, revision integer)
    LANGUAGE sql STABLE ROWS 1 SET search_path = public, pg_temp
AS $$
    WITH last AS (
        SELECT c.execution_id
        FROM test_execution_case c JOIN test_execution e ON e.id = c.execution_id
        WHERE c.requirement_id = target AND c.outcome IS NOT NULL AND e.state <> 'aborted'
        ORDER BY c.recorded_at DESC, c.execution_id DESC
        LIMIT 1
    )
    SELECT CASE WHEN bool_or(c.outcome = 'failed') THEN 'failed'
                WHEN bool_or(c.outcome = 'blocked') THEN 'blocked'
                WHEN bool_or(c.outcome IS NULL) THEN 'none'
                WHEN bool_and(c.outcome = 'skipped') THEN 'skipped'
                ELSE 'passed' END,
           last.execution_id, max(c.revision)
    FROM last JOIN test_execution_case c ON c.execution_id = last.execution_id AND c.requirement_id = target
    GROUP BY last.execution_id
$$;

-- The live test cases verifying a requirement through links that count as verification, as the reader sees them,
-- each with its link's suspicion and its last result.
CREATE FUNCTION requirement_verifying_cases(target uuid)
    RETURNS TABLE (case_id uuid, current integer, suspect boolean, outcome text, revision integer)
    LANGUAGE sql STABLE ROWS 10 SET search_path = public, pg_temp
AS $$
    SELECT s.id, s.revision, v.suspect, COALESCE(x.outcome, 'none'), x.revision
    FROM trace_link_state v
    JOIN link_type lt ON lt.id = v.type_id AND lt.counts = 'verification'
    JOIN requirement s ON s.id = v.source_id AND s.kind = 'requirement' AND s.trashed_at IS NULL
    JOIN requirement_type ty ON ty.id = s.type_id AND ty.role = 'testCase'
    LEFT JOIN LATERAL test_case_last_run(s.id) x ON true
    WHERE v.target_id = target
$$;

-- How many test cases verify a requirement, and how many of them last passed at their current revision through a
-- link that is not suspect.
CREATE FUNCTION requirement_case_counts(target uuid) RETURNS TABLE (cases integer, cases_whole integer)
    LANGUAGE sql STABLE ROWS 1 SET search_path = public, pg_temp
AS $$
    SELECT count(*)::int, count(*) FILTER (WHERE c.outcome = 'passed' AND c.revision >= c.current AND NOT c.suspect)::int
    FROM requirement_verifying_cases(target) c
$$;

-- The worst last result of the test cases verifying a requirement, for the query language's casesResult.
CREATE FUNCTION requirement_cases_result(target uuid) RETURNS text
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    SELECT COALESCE((SELECT c.outcome FROM requirement_verifying_cases(target) c
                     ORDER BY array_position(ARRAY['failed', 'blocked', 'none', 'skipped', 'passed'], c.outcome) LIMIT 1), 'none')
$$;

-- Fills the open executions of a run's project as its uploader lands it, when it is on the counting branch: each
-- iteration of a case the run verifies, not recorded by a tester nor by a newer upload, takes the worst of the run's
-- results for the case, errored counting as failed. Answers each iteration it filled.
CREATE FUNCTION test_execution_take_ci(ci_run uuid) RETURNS TABLE (filled_execution uuid, filled_case uuid)
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    run test_run%ROWTYPE;
BEGIN
    SELECT * INTO run FROM test_run WHERE id = ci_run;
    IF run.id IS NULL OR run.state <> 'running' OR run.uploaded_by IS DISTINCT FROM current_actor_id() THEN
        RAISE EXCEPTION 'only the uploader of a landing run fills executions with it'
            USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'test_execution_take_ci';
    END IF;
    IF run.branch <> evidence_counting_branch(run.project_id) THEN
        RETURN;
    END IF;
    RETURN QUERY
    WITH worst AS (
        SELECT DISTINCT ON (tr.requirement_id) tr.requirement_id AS id,
               CASE tr.outcome WHEN 'errored' THEN 'failed' ELSE tr.outcome END AS outcome, tr.suite, tr.name
        FROM test_result tr
        WHERE tr.run_id = ci_run
        ORDER BY tr.requirement_id, array_position(ARRAY['errored', 'failed', 'skipped', 'passed'], tr.outcome), tr.suite, tr.name
    )
    UPDATE test_execution_case c
    SET outcome = w.outcome, source = 'ci', recorded_by = run.uploaded_by, recorded_at = now(), ci_run_id = run.id,
        ci_suite = w.suite, ci_name = w.name, ci_commit = run.commit_sha, ci_uploaded_at = run.uploaded_at
    FROM worst w, test_execution e
    WHERE c.requirement_id = w.id AND e.id = c.execution_id AND e.project_id = run.project_id AND e.state = 'open'
      AND project_open(e.project_id) AND c.source IS DISTINCT FROM 'tester'
      AND (c.ci_uploaded_at IS NULL OR c.ci_uploaded_at <= run.uploaded_at)
    RETURNING c.execution_id, c.requirement_id;
END;
$$;

-- A piece of evidence as the hash writes it after its requirement's id, ordered byte by byte as Go orders it.
CREATE OR REPLACE FUNCTION baseline_evidence_line(e baseline_evidence) RETURNS text
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT CASE e.kind
        WHEN 'code' THEN format('code %s %s %s %s %s', e.repository, e.path, e.line, e.word, e.revision)
        WHEN 'run' THEN format('run %s %s %s', e.execution_id, e.revision, e.outcome)
        ELSE format('test %s %s %s %s %s %s', e.repository, e.job, e.suite, e.name, e.revision, e.outcome)
    END
$$;

-- Verified counts test cases by their last results too, where a rule names runs (#252).
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
               COALESCE(own.verified_from, every.verified_from, ARRAY['links']::text[]) AS verified_from,
               COALESCE(own.implemented_from, every.implemented_from, ARRAY['links']::text[]) AS implemented_from
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
    ), k AS (
        SELECT COALESCE(c.implementing, 0) AS implementing, COALESCE(c.implementing_whole, 0) AS implementing_whole,
               COALESCE(c.verifying, 0) AS verifying, COALESCE(c.verifying_whole, 0) AS verifying_whole
        FROM (SELECT 1) one LEFT JOIN requirement_code_counts(target) c ON true
    ), x AS (
        SELECT c.cases, c.cases_whole FROM requirement_case_counts(target) c
    ), v AS (
        SELECT 'links' = ANY (req.verified_from) AND t.verifying + a.verifying > 0 AS links_hold,
               t.verifying_suspect + a.verifying_suspect = 0 AND a.verifying_done = a.verifying AS links_whole,
               'tests' = ANY (req.verified_from) AND e.tests > 0 AS tests_hold,
               e.tests_whole = e.tests AS tests_whole,
               'code' = ANY (req.verified_from) AND k.verifying > 0 AS code_hold,
               k.verifying_whole = k.verifying AS code_whole,
               'runs' = ANY (req.verified_from) AND x.cases > 0 AS runs_hold,
               x.cases_whole = x.cases AS runs_whole,
               'links' = ANY (req.implemented_from) AND a.implementing > 0 AS impl_links_hold,
               a.implementing_done > 0 AND a.implementing_suspect = 0 AS impl_links_whole,
               'code' = ANY (req.implemented_from) AND k.implementing > 0 AS impl_code_hold,
               k.implementing_whole = k.implementing AS impl_code_whole
        FROM req, t, a, e, k, x
    )
    SELECT CASE
        WHEN req.kind <> 'requirement' OR NOT req.asks THEN 'na'
        WHEN dimension = 'decomposed' THEN
            CASE WHEN t.decomposing = 0 THEN 'none' WHEN t.decomposing_suspect = 0 THEN 'full' ELSE 'partial' END
        WHEN dimension = 'implemented' THEN
            CASE WHEN NOT (v.impl_links_hold OR v.impl_code_hold) THEN 'none'
                 WHEN (NOT v.impl_links_hold OR v.impl_links_whole) AND (NOT v.impl_code_hold OR v.impl_code_whole) THEN 'full'
                 ELSE 'partial' END
        ELSE
            CASE WHEN NOT (v.links_hold OR v.tests_hold OR v.code_hold OR v.runs_hold) THEN 'none'
                 WHEN (NOT v.links_hold OR v.links_whole) AND (NOT v.tests_hold OR v.tests_whole)
                      AND (NOT v.code_hold OR v.code_whole) AND (NOT v.runs_hold OR v.runs_whole) THEN 'full'
                 ELSE 'partial' END
    END
    FROM req, t, v
$$;
-- +goose StatementEnd

-- Each count answers one row; left at the default thousand, a coverage query costs enough to be compiled on every
-- call, which takes longer than running it.
ALTER FUNCTION requirement_test_counts(uuid) ROWS 1;
ALTER FUNCTION requirement_code_counts(uuid) ROWS 1;

REVOKE ALL ON FUNCTION test_execution_take_ci(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION test_execution_take_ci(uuid) TO exciter_app, exciter_admin;

-- +goose Down
-- +goose StatementBegin
-- A piece of evidence as the hash writes it after its requirement's id, ordered byte by byte as Go orders it.
CREATE OR REPLACE FUNCTION baseline_evidence_line(e baseline_evidence) RETURNS text
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT CASE e.kind
        WHEN 'code' THEN format('code %s %s %s %s %s', e.repository, e.path, e.line, e.word, e.revision)
        ELSE format('test %s %s %s %s %s %s', e.repository, e.job, e.suite, e.name, e.revision, e.outcome)
    END
$$;

-- Implemented and verified count links, test results and code tags (#200, #201).
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
               COALESCE(own.verified_from, every.verified_from, ARRAY['links']::text[]) AS verified_from,
               COALESCE(own.implemented_from, every.implemented_from, ARRAY['links']::text[]) AS implemented_from
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
    ), k AS (
        SELECT COALESCE(c.implementing, 0) AS implementing, COALESCE(c.implementing_whole, 0) AS implementing_whole,
               COALESCE(c.verifying, 0) AS verifying, COALESCE(c.verifying_whole, 0) AS verifying_whole
        FROM (SELECT 1) one LEFT JOIN requirement_code_counts(target) c ON true
    ), v AS (
        SELECT 'links' = ANY (req.verified_from) AND t.verifying + a.verifying > 0 AS links_hold,
               t.verifying_suspect + a.verifying_suspect = 0 AND a.verifying_done = a.verifying AS links_whole,
               'tests' = ANY (req.verified_from) AND e.tests > 0 AS tests_hold,
               e.tests_whole = e.tests AS tests_whole,
               'code' = ANY (req.verified_from) AND k.verifying > 0 AS code_hold,
               k.verifying_whole = k.verifying AS code_whole,
               'links' = ANY (req.implemented_from) AND a.implementing > 0 AS impl_links_hold,
               a.implementing_done > 0 AND a.implementing_suspect = 0 AS impl_links_whole,
               'code' = ANY (req.implemented_from) AND k.implementing > 0 AS impl_code_hold,
               k.implementing_whole = k.implementing AS impl_code_whole
        FROM req, t, a, e, k
    )
    SELECT CASE
        WHEN req.kind <> 'requirement' OR NOT req.asks THEN 'na'
        WHEN dimension = 'decomposed' THEN
            CASE WHEN t.decomposing = 0 THEN 'none' WHEN t.decomposing_suspect = 0 THEN 'full' ELSE 'partial' END
        WHEN dimension = 'implemented' THEN
            CASE WHEN NOT (v.impl_links_hold OR v.impl_code_hold) THEN 'none'
                 WHEN (NOT v.impl_links_hold OR v.impl_links_whole) AND (NOT v.impl_code_hold OR v.impl_code_whole) THEN 'full'
                 ELSE 'partial' END
        ELSE
            CASE WHEN NOT (v.links_hold OR v.tests_hold OR v.code_hold) THEN 'none'
                 WHEN (NOT v.links_hold OR v.links_whole) AND (NOT v.tests_hold OR v.tests_whole) AND (NOT v.code_hold OR v.code_whole) THEN 'full'
                 ELSE 'partial' END
    END
    FROM req, t, v
$$;
-- +goose StatementEnd
ALTER FUNCTION requirement_test_counts(uuid) ROWS 1000;
ALTER FUNCTION requirement_code_counts(uuid) ROWS 1000;
DROP FUNCTION IF EXISTS test_execution_take_ci(uuid);
DROP FUNCTION IF EXISTS requirement_cases_result(uuid);
DROP FUNCTION IF EXISTS requirement_case_counts(uuid);
DROP FUNCTION IF EXISTS requirement_verifying_cases(uuid);
DROP FUNCTION IF EXISTS test_case_last_run(uuid);
DELETE FROM baseline_evidence WHERE kind = 'run';
ALTER TABLE baseline_evidence DROP COLUMN IF EXISTS execution_id,
    DROP CONSTRAINT baseline_evidence_kind_check, ADD CONSTRAINT baseline_evidence_kind_check CHECK (kind IN ('test', 'code'));
UPDATE coverage_rule SET verified_from = COALESCE(NULLIF(array_remove(verified_from, 'runs'), '{}'), ARRAY['links']::text[])
WHERE 'runs' = ANY (verified_from);
ALTER TABLE coverage_rule DROP CONSTRAINT coverage_rule_verified_from,
    ADD CONSTRAINT coverage_rule_verified_from CHECK (cardinality(verified_from) >= 1 AND verified_from <@ ARRAY['links', 'tests', 'code']::text[]);
