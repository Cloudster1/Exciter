-- +goose Up
-- A project's risk scheme (#259), as docs/api-contract-m13.md agrees: the scales a risk is rated on, the levels a
-- rating gives, and the matrix or the priority-number thresholds that give it. A level is never stored with a
-- rating; it is worked out here whenever it is read.

-- risk.MaxLabelLength, risk.MaxNameLength, risk.MaxDescriptionLength and risk.MaxScaleValue.
CREATE TABLE risk_scheme (
    project_id        uuid PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    method            text NOT NULL CHECK (method IN ('matrix', 'rpn')),
    detection         boolean NOT NULL DEFAULT false,
    severity_label    text NOT NULL CHECK (char_length(severity_label) BETWEEN 1 AND 50),
    probability_label text NOT NULL CHECK (char_length(probability_label) BETWEEN 1 AND 50),
    detection_label   text NOT NULL CHECK (char_length(detection_label) BETWEEN 1 AND 50),
    updated_by        uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE risk_scale_level (
    project_id  uuid NOT NULL REFERENCES risk_scheme(project_id) ON DELETE CASCADE,
    dimension   text NOT NULL CHECK (dimension IN ('severity', 'probability', 'detection')),
    value       smallint NOT NULL CHECK (value BETWEEN 1 AND 10),
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    PRIMARY KEY (project_id, dimension, value)
);

-- risk.MaxLevels; position orders them from the least to the most severe.
CREATE TABLE risk_level (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id uuid NOT NULL REFERENCES risk_scheme(project_id) ON DELETE CASCADE,
    position   smallint NOT NULL CHECK (position BETWEEN 0 AND 9),
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    colour     text NOT NULL CHECK (colour ~ '^#[0-9a-f]{6}$'),
    acceptable boolean NOT NULL DEFAULT false,
    min_rpn    integer CHECK (min_rpn BETWEEN 1 AND 1000),
    UNIQUE (project_id, position)
);
CREATE UNIQUE INDEX risk_level_name_idx ON risk_level (project_id, lower(name));

CREATE TABLE risk_matrix_cell (
    project_id  uuid NOT NULL REFERENCES risk_scheme(project_id) ON DELETE CASCADE,
    severity    smallint NOT NULL CHECK (severity BETWEEN 1 AND 10),
    probability smallint NOT NULL CHECK (probability BETWEEN 1 AND 10),
    level_id    uuid NOT NULL REFERENCES risk_level(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, severity, probability)
);

-- +goose StatementBegin
-- Whether the person administers the scheme's project and it is not archived, as for coverage rules (#38).
CREATE FUNCTION risk_scheme_keeper(target_project uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_project_holds(target_project, actor, 'administer')
           AND EXISTS (SELECT 1 FROM project p WHERE p.id = target_project AND p.archived_at IS NULL);
$$;

-- The priority number of one side of a rating: the product of its values, detection only when the scheme rates
-- it; null while a value it needs is unknown.
CREATE FUNCTION risk_rpn(side jsonb, detection boolean) RETURNS integer
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT (side->>'severity')::integer * (side->>'probability')::integer
           * CASE WHEN detection THEN (side->>'detection')::integer ELSE 1 END
$$;

-- The level one side of a rating gives under its project's scheme: the matrix's cell, or the highest level whose
-- threshold the priority number reaches. Plain SQL, so a query over many risks inlines it.
CREATE FUNCTION risk_level_of(target_project uuid, side jsonb) RETURNS uuid
    LANGUAGE sql STABLE
AS $$
    SELECT CASE s.method
        WHEN 'matrix' THEN (
            SELECT c.level_id FROM risk_matrix_cell c
            WHERE c.project_id = s.project_id
              AND c.severity = (side->>'severity')::integer AND c.probability = (side->>'probability')::integer)
        ELSE (
            SELECT l.id FROM risk_level l
            WHERE l.project_id = s.project_id AND COALESCE(l.min_rpn, 0) <= risk_rpn(side, s.detection)
            ORDER BY l.position DESC LIMIT 1)
    END
    FROM risk_scheme s WHERE s.project_id = target_project
$$;

-- A level as a rating answers it: by name, with its colour and whether it is acceptable; null for none.
CREATE FUNCTION risk_level_ref(level uuid) RETURNS jsonb
    LANGUAGE sql STABLE
AS $$
    SELECT jsonb_build_object('name', l.name, 'colour', l.colour, 'acceptable', l.acceptable)
    FROM risk_level l WHERE l.id = level
$$;
-- +goose StatementEnd

ALTER TABLE risk_scheme ENABLE ROW LEVEL SECURITY;
ALTER TABLE risk_scheme FORCE ROW LEVEL SECURITY;
ALTER TABLE risk_scale_level ENABLE ROW LEVEL SECURITY;
ALTER TABLE risk_scale_level FORCE ROW LEVEL SECURITY;
ALTER TABLE risk_level ENABLE ROW LEVEL SECURITY;
ALTER TABLE risk_level FORCE ROW LEVEL SECURITY;
ALTER TABLE risk_matrix_cell ENABLE ROW LEVEL SECURITY;
ALTER TABLE risk_matrix_cell FORCE ROW LEVEL SECURITY;

-- One open policy per table, as every table has since #106, and the permission policies beside it: a scheme is
-- read by whoever views its project and set by whoever administers it.
CREATE POLICY risk_scheme_installation ON risk_scheme USING (true) WITH CHECK (true);
CREATE POLICY risk_scale_level_installation ON risk_scale_level USING (true) WITH CHECK (true);
CREATE POLICY risk_level_installation ON risk_level USING (true) WITH CHECK (true);
CREATE POLICY risk_matrix_cell_installation ON risk_matrix_cell USING (true) WITH CHECK (true);

CREATE POLICY risk_scheme_readers ON risk_scheme AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY risk_scheme_setters ON risk_scheme AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (risk_scheme_keeper(project_id, current_actor_id()));
CREATE POLICY risk_scheme_changers ON risk_scheme AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (risk_scheme_keeper(project_id, current_actor_id())) WITH CHECK (risk_scheme_keeper(project_id, current_actor_id()));
CREATE POLICY risk_scheme_removers ON risk_scheme AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (risk_scheme_keeper(project_id, current_actor_id()));

CREATE POLICY risk_scale_level_readers ON risk_scale_level AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY risk_scale_level_setters ON risk_scale_level AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (risk_scheme_keeper(project_id, current_actor_id()));
CREATE POLICY risk_scale_level_changers ON risk_scale_level AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (risk_scheme_keeper(project_id, current_actor_id())) WITH CHECK (risk_scheme_keeper(project_id, current_actor_id()));
CREATE POLICY risk_scale_level_removers ON risk_scale_level AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (risk_scheme_keeper(project_id, current_actor_id()));

CREATE POLICY risk_level_readers ON risk_level AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY risk_level_setters ON risk_level AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (risk_scheme_keeper(project_id, current_actor_id()));
CREATE POLICY risk_level_changers ON risk_level AS RESTRICTIVE FOR UPDATE TO exciter_app
    USING (risk_scheme_keeper(project_id, current_actor_id())) WITH CHECK (risk_scheme_keeper(project_id, current_actor_id()));
CREATE POLICY risk_level_removers ON risk_level AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (risk_scheme_keeper(project_id, current_actor_id()));

CREATE POLICY risk_matrix_cell_readers ON risk_matrix_cell AS RESTRICTIVE FOR SELECT TO exciter_app
    USING (perm_project_holds(project_id, current_actor_id(), 'view'));
CREATE POLICY risk_matrix_cell_setters ON risk_matrix_cell AS RESTRICTIVE FOR INSERT TO exciter_app
    WITH CHECK (risk_scheme_keeper(project_id, current_actor_id()));
CREATE POLICY risk_matrix_cell_removers ON risk_matrix_cell AS RESTRICTIVE FOR DELETE TO exciter_app
    USING (risk_scheme_keeper(project_id, current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON risk_scheme, risk_scale_level, risk_level, risk_matrix_cell TO exciter_admin;
REVOKE ALL ON risk_scheme, risk_scale_level, risk_level, risk_matrix_cell FROM exciter_app;
GRANT SELECT, INSERT, DELETE ON risk_scheme, risk_scale_level, risk_level, risk_matrix_cell TO exciter_app;
GRANT UPDATE (method, detection, severity_label, probability_label, detection_label, updated_by, updated_at) ON risk_scheme TO exciter_app;
GRANT UPDATE (name, description) ON risk_scale_level TO exciter_app;
GRANT UPDATE (position, name, colour, acceptable, min_rpn) ON risk_level TO exciter_app;
REVOKE ALL ON FUNCTION risk_scheme_keeper(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION risk_scheme_keeper(uuid, uuid) TO exciter_app, exciter_admin;

-- +goose Down
DROP FUNCTION risk_level_ref(uuid);
DROP FUNCTION risk_level_of(uuid, jsonb);
DROP FUNCTION risk_rpn(jsonb, boolean);
DROP TABLE risk_matrix_cell;
DROP TABLE risk_level;
DROP TABLE risk_scale_level;
DROP TABLE risk_scheme;
DROP FUNCTION risk_scheme_keeper(uuid, uuid);
