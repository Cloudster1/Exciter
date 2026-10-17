-- +goose Up
-- Code tags (#201), as docs/api-contract-m9.md agrees: a scan of a repository uploads the tags its source comments
-- carry, such as [implements CHK-R42@3], one scan per repository and branch. A tag keeps the word it was written with
-- and is read by the project's words of the day; it names the revision it was written against, and is outdated once
-- the requirement is revised past it, or orphaned when its requirement is in the trash or there is none.

-- The words a project's tags use; without a row, implements and verifies.
CREATE TABLE code_tag_words (
    project_id uuid PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    -- codetrace.MaxWords and MaxWordLength, each a letter followed by letters, digits, - or _.
    implements text[] NOT NULL CHECK (cardinality(implements) BETWEEN 1 AND 10),
    verifies   text[] NOT NULL CHECK (cardinality(verifies) BETWEEN 1 AND 10),
    updated_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT code_tag_words_shape CHECK (
        array_to_string(implements || verifies, ' ') ~ '^[A-Za-z][A-Za-z0-9_-]{0,29}( [A-Za-z][A-Za-z0-9_-]{0,29})*$'),
    CONSTRAINT code_tag_words_apart CHECK (NOT (lower(implements::text)::text[] && lower(verifies::text)::text[]))
);

-- A scan of one repository at one commit of a branch. The next scan of the branch replaces it: the scan before keeps
-- its row, which notifications name, and lets go of its tags.
CREATE TABLE code_scan (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id  uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    -- codetrace.MaxNameLength and MaxURLLength.
    repository  text NOT NULL CHECK (btrim(repository) <> '' AND char_length(repository) <= 200),
    commit_sha  text NOT NULL CHECK (commit_sha ~ '^[0-9a-f]{7,64}$'),
    branch      text NOT NULL CHECK (btrim(branch) <> '' AND char_length(branch) <= 200),
    file_url    text NOT NULL DEFAULT '' CHECK (file_url = '' OR (file_url ~* '^https?://[^\s]+$' AND char_length(file_url) <= 2000)),
    tags        integer NOT NULL DEFAULT 0 CHECK (tags >= 0),
    linked      integer NOT NULL DEFAULT 0 CHECK (linked >= 0),
    -- The tags that link nothing, [{path, line, word, key, revision, problem}], at most codetrace.MaxReported.
    reported    jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(reported) = 'array'),
    unreported  integer NOT NULL DEFAULT 0 CHECK (unreported >= 0),
    uploaded_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    uploaded_at timestamptz NOT NULL DEFAULT now(),
    replaced_at timestamptz,
    via_kind    text CHECK (via_kind IN ('token', 'oauth')),
    via_id      uuid,
    via_name    text,
    CONSTRAINT code_scan_via_whole CHECK ((via_kind IS NULL) = (via_id IS NULL) AND (via_kind IS NULL) = (via_name IS NULL))
);
CREATE UNIQUE INDEX code_scan_one_per_branch ON code_scan (project_id, repository, branch) WHERE replaced_at IS NULL;

-- One tag of a scan: where it sits, the word it was written with, the key it carried, and what that key reached.
CREATE TABLE code_tag (
    scan_id        uuid NOT NULL REFERENCES code_scan(id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    path           text NOT NULL CHECK (btrim(path) <> '' AND char_length(path) <= 1000),
    line           integer NOT NULL CHECK (line >= 1),
    word           text NOT NULL CHECK (char_length(word) BETWEEN 1 AND 30),
    key            text NOT NULL CHECK (char_length(key) <= 30),
    -- Null for a key that named nothing, or a requirement purged since.
    requirement_id uuid REFERENCES requirement(id) ON DELETE SET NULL,
    -- The revision the tag names, or carried from the scan before; 0 for a key that named nothing.
    revision       integer NOT NULL CHECK (revision >= 0),
    PRIMARY KEY (scan_id, path, line, word, key)
);
CREATE INDEX code_tag_requirement_idx ON code_tag (requirement_id, scan_id);

ALTER TABLE coverage_rule ADD COLUMN implemented_from text[] NOT NULL DEFAULT ARRAY['links']::text[],
    ADD CONSTRAINT coverage_rule_implemented_from CHECK (cardinality(implemented_from) >= 1 AND implemented_from <@ ARRAY['links', 'code']::text[]),
    DROP CONSTRAINT coverage_rule_verified_from,
    ADD CONSTRAINT coverage_rule_verified_from CHECK (cardinality(verified_from) >= 1 AND verified_from <@ ARRAY['links', 'tests', 'code']::text[]);

-- A code tag a baseline recorded sits beside its test results.
ALTER TABLE baseline_evidence DROP CONSTRAINT baseline_evidence_kind_check,
    ADD CONSTRAINT baseline_evidence_kind_check CHECK (kind IN ('test', 'code')),
    ADD COLUMN path text NOT NULL DEFAULT '',
    ADD COLUMN line integer NOT NULL DEFAULT 0,
    ADD COLUMN word text NOT NULL DEFAULT '';

-- A trace model may end in a level of code, reached by implements or verifies.
ALTER TABLE trace_level DROP CONSTRAINT trace_level_kind_check,
    ADD CONSTRAINT trace_level_kind_check CHECK (kind IN ('requirements', 'issues', 'pages', 'tests', 'code'));

-- A scan's notifications name it rather than a requirement.
ALTER TABLE notification ADD COLUMN code_scan_id uuid REFERENCES code_scan(id) ON DELETE CASCADE,
    DROP CONSTRAINT notification_about_something,
    ADD CONSTRAINT notification_about_something CHECK (requirement_id IS NOT NULL OR session_id IS NOT NULL OR code_scan_id IS NOT NULL),
    DROP CONSTRAINT notification_kind_check,
    ADD CONSTRAINT notification_kind_check CHECK (kind IN ('mentioned', 'review_requested', 'reviewed', 'suspect', 'replied',
        'commented', 'resolved', 'revised', 'transitioned', 'source_revised', 'session_invited', 'session_round',
        'session_reminder', 'session_closed', 'code_outdated', 'code_orphaned'));

-- +goose StatementBegin
-- The relation a word means in a project's tags, by its words of the day, or null for a word it does not use.
CREATE FUNCTION code_tag_relation(project uuid, word text) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE
        WHEN lower(word) = ANY (SELECT lower(w) FROM unnest(COALESCE(x.implements, ARRAY['implements'])) w) THEN 'implements'
        WHEN lower(word) = ANY (SELECT lower(w) FROM unnest(COALESCE(x.verifies, ARRAY['verifies'])) w) THEN 'verifies'
    END
    FROM (SELECT 1) one LEFT JOIN code_tag_words x ON x.project_id = project
$$;

-- A requirement's code tags on the branch evidence is read on, as the reader sees them, by relation: how many, and
-- how many name its current revision.
CREATE FUNCTION requirement_code_counts(target uuid)
    RETURNS TABLE (implementing integer, implementing_whole integer, verifying integer, verifying_whole integer, outdated integer)
    LANGUAGE sql STABLE SET search_path = public, pg_temp
AS $$
    SELECT count(*) FILTER (WHERE x.relation = 'implements')::int,
           count(*) FILTER (WHERE x.relation = 'implements' AND x.revision >= r.revision)::int,
           count(*) FILTER (WHERE x.relation = 'verifies')::int,
           count(*) FILTER (WHERE x.relation = 'verifies' AND x.revision >= r.revision)::int,
           count(*) FILTER (WHERE x.revision < r.revision)::int
    FROM requirement r
    JOIN code_scan s ON s.project_id = r.project_id AND s.branch = evidence_branch(r.project_id) AND s.replaced_at IS NULL
    CROSS JOIN LATERAL (SELECT t.revision, code_tag_relation(r.project_id, t.word) AS relation
                        FROM code_tag t WHERE t.scan_id = s.id AND t.requirement_id = r.id) x
    WHERE r.id = target AND x.relation IS NOT NULL
$$;

-- An upload replaces its branch's scan: the scan before goes, with its tags, and the new one is its uploader's, in
-- a project they edit. The tags follow as the uploader in the same transaction.
CREATE FUNCTION code_scan_replace(project uuid, repo text, commit_sha text, branch_name text, file_url text) RETURNS uuid
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    made uuid;
BEGIN
    IF actor IS NULL OR NOT perm_project_holds(project, actor, 'edit') THEN
        RAISE EXCEPTION 'code_scan_forbidden' USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'code_scan_forbidden',
            DETAIL = 'A scan is uploaded by an editor of an open project, in their own name.';
    END IF;
    DELETE FROM code_tag WHERE scan_id IN (SELECT id FROM code_scan WHERE project_id = project AND repository = repo
                                           AND branch = branch_name AND replaced_at IS NULL);
    UPDATE code_scan SET replaced_at = now() WHERE project_id = project AND repository = repo AND branch = branch_name AND replaced_at IS NULL;
    INSERT INTO code_scan (project_id, repository, commit_sha, branch, file_url, uploaded_by)
    VALUES (project, repo, commit_sha, branch_name, file_url, actor) RETURNING id INTO made;
    RETURN made;
END;
$$;

-- What a scan is stays what it was uploaded: its counts are written as it is made, and it is replaced once.
CREATE FUNCTION code_scan_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.project_id, NEW.repository, NEW.commit_sha, NEW.branch, NEW.file_url, NEW.uploaded_by, NEW.uploaded_at)
       IS DISTINCT FROM (OLD.project_id, OLD.repository, OLD.commit_sha, OLD.branch, OLD.file_url, OLD.uploaded_by, OLD.uploaded_at)
       OR ((NEW.tags, NEW.linked, NEW.reported, NEW.unreported) IS DISTINCT FROM (OLD.tags, OLD.linked, OLD.reported, OLD.unreported)
           AND OLD.uploaded_at <> now())
       OR (OLD.replaced_at IS NOT NULL AND NEW.replaced_at IS DISTINCT FROM OLD.replaced_at) THEN
        RAISE EXCEPTION 'code_scan_fixed' USING ERRCODE = 'check_violation', CONSTRAINT = 'code_scan_fixed',
            DETAIL = 'A scan stays what it was uploaded.';
    END IF;
    RETURN NEW;
END;
$$;

-- A piece of evidence as the hash writes it after its requirement's id, ordered byte by byte as Go orders it.
CREATE OR REPLACE FUNCTION baseline_evidence_line(e baseline_evidence) RETURNS text
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT CASE e.kind
        WHEN 'code' THEN format('code %s %s %s %s %s', e.repository, e.path, e.line, e.word, e.revision)
        ELSE format('test %s %s %s %s %s %s', e.repository, e.job, e.suite, e.name, e.revision, e.outcome)
    END
$$;
-- +goose StatementEnd

CREATE TRIGGER code_scan_guard BEFORE UPDATE ON code_scan FOR EACH ROW EXECUTE FUNCTION code_scan_guard();
CREATE TRIGGER code_scan_via BEFORE INSERT OR UPDATE ON code_scan FOR EACH ROW EXECUTE FUNCTION via_stamped();

ALTER TABLE code_tag_words ENABLE ROW LEVEL SECURITY;
ALTER TABLE code_tag_words FORCE  ROW LEVEL SECURITY;
ALTER TABLE code_scan      ENABLE ROW LEVEL SECURITY;
ALTER TABLE code_scan      FORCE  ROW LEVEL SECURITY;
ALTER TABLE code_tag       ENABLE ROW LEVEL SECURITY;
ALTER TABLE code_tag       FORCE  ROW LEVEL SECURITY;

CREATE POLICY code_tag_words_installation ON code_tag_words USING (true) WITH CHECK (true);
CREATE POLICY code_scan_installation ON code_scan USING (true) WITH CHECK (true);
CREATE POLICY code_tag_installation ON code_tag USING (true) WITH CHECK (true);

-- Whoever views a project reads its tag words; its administrators of an open project set them.
CREATE POLICY code_tag_words_readers ON code_tag_words AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY code_tag_words_keepers ON code_tag_words AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id)
                AND updated_by = current_actor_id());
CREATE POLICY code_tag_words_changers ON code_tag_words AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'administer') AND project_open(project_id))
    WITH CHECK (perm_project_holds(project_id, current_actor_id(), 'administer') AND updated_by = current_actor_id());

-- Whoever views a project reads its scans and tags; a scan is made only through code_scan_replace, and its tags and
-- counts written by its uploader in the transaction that made it.
CREATE POLICY code_scan_readers ON code_scan AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY code_scan_counters ON code_scan AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (uploaded_by = current_actor_id() AND uploaded_at = now()) WITH CHECK (uploaded_by = current_actor_id());
CREATE POLICY code_tag_readers ON code_tag AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY code_tag_writers ON code_tag AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM code_scan s WHERE s.id = scan_id AND s.project_id = code_tag.project_id
                        AND s.uploaded_by = current_actor_id() AND s.uploaded_at = now() AND s.replaced_at IS NULL)
                AND (requirement_id IS NULL OR EXISTS (SELECT 1 FROM requirement q WHERE q.id = requirement_id
                     AND q.project_id = code_tag.project_id AND q.kind = 'requirement' AND code_tag.revision BETWEEN 1 AND q.revision)));

-- A notification about a scan is for whoever may read its project.
DROP POLICY notification_own_read ON notification;
DROP POLICY notification_own_write ON notification;
CREATE POLICY notification_own_read ON notification AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (user_id = current_actor_id() AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id()))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM code_scan c WHERE c.id = code_scan_id
                                               AND perm_project_holds(c.project_id, current_actor_id(), 'view')))));
CREATE POLICY notification_own_write ON notification AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (user_id = current_actor_id() AND read_at IS NULL AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id()))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM code_scan c WHERE c.id = code_scan_id
                                               AND perm_project_holds(c.project_id, current_actor_id(), 'view')))));

GRANT SELECT, INSERT, UPDATE, DELETE ON code_tag_words, code_scan, code_tag TO exciter_admin;
REVOKE ALL ON code_tag_words, code_scan, code_tag FROM exciter_app;
GRANT SELECT, INSERT ON code_tag_words, code_tag TO exciter_app;
GRANT SELECT ON code_scan TO exciter_app;
GRANT UPDATE (implements, verifies, updated_by, updated_at) ON code_tag_words TO exciter_app;
GRANT UPDATE (tags, linked, reported, unreported) ON code_scan TO exciter_app;
GRANT UPDATE (implemented_from) ON coverage_rule TO exciter_app;
REVOKE ALL ON FUNCTION code_tag_relation(uuid, text), code_scan_replace(uuid, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION code_tag_relation(uuid, text), code_scan_replace(uuid, text, text, text, text) TO exciter_app, exciter_admin;

-- +goose StatementBegin
-- Implemented and verified count the sources a rule lists (#200, #201): links as before, test results, and code tags
-- naming the current revision to be whole.
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

CREATE OR REPLACE FUNCTION baseline_evidence_line(e baseline_evidence) RETURNS text
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT format('test %s %s %s %s %s %s', e.repository, e.job, e.suite, e.name, e.revision, e.outcome)
$$;
-- +goose StatementEnd
DROP POLICY IF EXISTS notification_own_read ON notification;
DROP POLICY IF EXISTS notification_own_write ON notification;
DELETE FROM notification WHERE code_scan_id IS NOT NULL OR kind LIKE 'code_%';
CREATE POLICY notification_own_read ON notification AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (user_id = current_actor_id() AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id()))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))));
CREATE POLICY notification_own_write ON notification AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (user_id = current_actor_id() AND read_at IS NULL AND (
        (requirement_id IS NOT NULL AND perm_requirement_viewable(requirement_id, current_actor_id()))
        OR (requirement_id IS NULL AND EXISTS (SELECT 1 FROM review_session s WHERE s.id = session_id
                                               AND perm_project_holds(s.project_id, current_actor_id(), 'view')))));
ALTER TABLE notification DROP CONSTRAINT notification_kind_check,
    ADD CONSTRAINT notification_kind_check CHECK (kind IN ('mentioned', 'review_requested', 'reviewed', 'suspect', 'replied',
        'commented', 'resolved', 'revised', 'transitioned', 'source_revised', 'session_invited', 'session_round',
        'session_reminder', 'session_closed')),
    DROP CONSTRAINT notification_about_something,
    ADD CONSTRAINT notification_about_something CHECK (requirement_id IS NOT NULL OR session_id IS NOT NULL),
    DROP COLUMN code_scan_id;
DELETE FROM baseline_evidence WHERE kind = 'code';
ALTER TABLE baseline_evidence DROP COLUMN IF EXISTS word, DROP COLUMN IF EXISTS line, DROP COLUMN IF EXISTS path,
    DROP CONSTRAINT baseline_evidence_kind_check, ADD CONSTRAINT baseline_evidence_kind_check CHECK (kind IN ('test'));
ALTER TABLE coverage_rule DROP CONSTRAINT coverage_rule_implemented_from, DROP COLUMN implemented_from,
    DROP CONSTRAINT coverage_rule_verified_from,
    ADD CONSTRAINT coverage_rule_verified_from CHECK (cardinality(verified_from) >= 1 AND verified_from <@ ARRAY['links', 'tests']::text[]);
DELETE FROM trace_level WHERE kind = 'code';
ALTER TABLE trace_level DROP CONSTRAINT trace_level_kind_check,
    ADD CONSTRAINT trace_level_kind_check CHECK (kind IN ('requirements', 'issues', 'pages', 'tests'));
DROP TABLE IF EXISTS code_tag;
DROP TABLE IF EXISTS code_scan;
DROP TABLE IF EXISTS code_tag_words;
DROP FUNCTION IF EXISTS code_scan_guard();
DROP FUNCTION IF EXISTS code_scan_replace(uuid, text, text, text, text);
DROP FUNCTION IF EXISTS requirement_code_counts(uuid);
DROP FUNCTION IF EXISTS code_tag_relation(uuid, text);
