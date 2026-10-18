-- +goose Up
-- Baselines record the evidence they held (#200): each latest test result of the counting branch, as a frozen copy
-- with its commit and address inline, so pruning a run changes no baseline. The signed content hash covers it after
-- its other lines, and a baseline without evidence keeps the hash it was signed with.

CREATE TABLE baseline_evidence (
    baseline_id    uuid NOT NULL REFERENCES baseline(id) ON DELETE CASCADE,
    id             uuid NOT NULL DEFAULT uuidv7(),
    requirement_id uuid NOT NULL,
    kind           text NOT NULL CHECK (kind IN ('test')),
    repository     text NOT NULL,
    branch         text NOT NULL,
    job            text NOT NULL DEFAULT '',
    suite          text NOT NULL DEFAULT '',
    name           text NOT NULL,
    outcome        text NOT NULL,
    revision       integer NOT NULL,
    commit_sha     text NOT NULL,
    url            text NOT NULL DEFAULT '',
    PRIMARY KEY (baseline_id, id)
);
CREATE INDEX baseline_evidence_requirement_idx ON baseline_evidence (baseline_id, requirement_id);

CREATE TRIGGER baseline_evidence_frozen BEFORE INSERT OR UPDATE OR DELETE ON baseline_evidence
    FOR EACH ROW EXECUTE FUNCTION baseline_rows_frozen();

ALTER TABLE baseline_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE baseline_evidence FORCE  ROW LEVEL SECURITY;
CREATE POLICY baseline_evidence_installation ON baseline_evidence USING (true) WITH CHECK (true);
-- As the other rows a baseline holds: shown with it, written by whoever took it.
CREATE POLICY baseline_evidence_readers ON baseline_evidence AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id));
CREATE POLICY baseline_evidence_takers ON baseline_evidence AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (EXISTS (SELECT 1 FROM baseline b WHERE b.id = baseline_id AND b.created_by = current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON baseline_evidence TO exciter_admin;
REVOKE ALL ON baseline_evidence FROM exciter_app;
GRANT SELECT, INSERT ON baseline_evidence TO exciter_app;

-- +goose StatementBegin
-- A piece of evidence as the hash writes it after its requirement's id, ordered byte by byte as Go orders it.
CREATE FUNCTION baseline_evidence_line(e baseline_evidence) RETURNS text
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT format('test %s %s %s %s %s %s', e.repository, e.job, e.suite, e.name, e.revision, e.outcome)
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
                     FROM baseline_page_link g WHERE g.baseline_id = bid), ''),
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
$$;
-- +goose StatementEnd
DROP FUNCTION IF EXISTS baseline_evidence_line(baseline_evidence);
DROP TABLE IF EXISTS baseline_evidence;
