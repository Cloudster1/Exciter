-- +goose Up
-- Risks in traceability and baselines (#261), as docs/api-contract-m13.md agrees: a baseline records each risk it
-- holds as its rating stood, with its levels, state and mitigations, so a scheme changed later changes nothing it
-- recorded. A baseline without risks keeps the hash it was signed with.

-- What a risk or a mitigation stood at when taken: a risk's priority numbers, levels and state as risk_view answers
-- them, a mitigation's source, coverage and whether it was in effect.
ALTER TABLE baseline_evidence DROP CONSTRAINT baseline_evidence_kind_check,
    ADD CONSTRAINT baseline_evidence_kind_check CHECK (kind IN ('test', 'code', 'run', 'risk', 'mitigation')),
    ADD COLUMN detail jsonb,
    ADD CONSTRAINT baseline_evidence_detail_shape CHECK ((kind IN ('risk', 'mitigation')) = (detail IS NOT NULL));

-- +goose StatementBegin
-- A piece of evidence as the hash writes it after its requirement's id, ordered byte by byte as Go orders it; a
-- risk's and a mitigation's detail as jsonb writes it, which orders its keys the same way every time.
CREATE OR REPLACE FUNCTION baseline_evidence_line(e baseline_evidence) RETURNS text
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT CASE e.kind
        WHEN 'code' THEN format('code %s %s %s %s %s', e.repository, e.path, e.line, e.word, e.revision)
        WHEN 'run' THEN format('run %s %s %s', e.execution_id, e.revision, e.outcome)
        WHEN 'risk' THEN 'risk ' || e.detail::text
        WHEN 'mitigation' THEN 'mitigation ' || e.detail::text
        ELSE format('test %s %s %s %s %s %s', e.repository, e.job, e.suite, e.name, e.revision, e.outcome)
    END
$$;

-- A risk's mark in the trace graph and the chain report: the level it stands at and its state.
CREATE FUNCTION risk_mark(target uuid) RETURNS jsonb
    LANGUAGE sql STABLE
AS $$
    SELECT jsonb_build_object('level', v->'level', 'state', v->'state')
    FROM requirement r
    JOIN requirement_type t ON t.id = r.type_id AND t.role = 'risk'
    CROSS JOIN LATERAL (SELECT risk_view(r.project_id, r.risk, r.id) AS v) x
    WHERE r.id = target AND r.kind = 'requirement'
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION baseline_evidence_line(e baseline_evidence) RETURNS text
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT CASE e.kind
        WHEN 'code' THEN format('code %s %s %s %s %s', e.repository, e.path, e.line, e.word, e.revision)
        WHEN 'run' THEN format('run %s %s %s', e.execution_id, e.revision, e.outcome)
        ELSE format('test %s %s %s %s %s %s', e.repository, e.job, e.suite, e.name, e.revision, e.outcome)
    END
$$;
-- +goose StatementEnd
DROP FUNCTION risk_mark(uuid);
DELETE FROM baseline_evidence WHERE kind IN ('risk', 'mitigation');
ALTER TABLE baseline_evidence DROP CONSTRAINT baseline_evidence_detail_shape, DROP COLUMN detail,
    DROP CONSTRAINT baseline_evidence_kind_check, ADD CONSTRAINT baseline_evidence_kind_check CHECK (kind IN ('test', 'code', 'run'));
