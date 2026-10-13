-- +goose Up
-- Full-text search over what a requirement says now, as Stator searches its pages: the title, weighted A, the
-- plain text of statement and rationale, weighted B, and its text and rich text values, weighted C, so a title
-- match ranks above the rest. Matching ignores case and accents but does not stem, because requirements are
-- written in more than one language.
CREATE EXTENSION IF NOT EXISTS unaccent;

CREATE TEXT SEARCH CONFIGURATION exciter_search (COPY = pg_catalog.simple);
ALTER TEXT SEARCH CONFIGURATION exciter_search
    ALTER MAPPING FOR hword, hword_part, word WITH unaccent, simple;

-- The words of a document the way document.PlainText reads them: one block per line, table cells separated by
-- tabs, mentions as @Name. It lives in the database so every writer keeps the index true, whatever statement it
-- runs; the integration suite holds the two to the same answer.
-- +goose StatementBegin
CREATE FUNCTION requirement_plain_blocks(blocks jsonb, depth integer) RETURNS text
    LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE
AS $$
DECLARE
    n      jsonb;
    c      jsonb;
    cell   jsonb;
    line   text;
    cells  text[];
    out    text := '';
BEGIN
    IF depth > 40 OR jsonb_typeof(blocks) IS DISTINCT FROM 'array' THEN
        RETURN '';
    END IF;
    FOR n IN SELECT value FROM jsonb_array_elements(blocks) LOOP
        CASE n->>'type'
        WHEN 'paragraph', 'heading', 'codeBlock' THEN
            line := '';
            IF jsonb_typeof(n->'content') = 'array' THEN
                FOR c IN SELECT value FROM jsonb_array_elements(n->'content') LOOP
                    CASE c->>'type'
                    WHEN 'text' THEN line := line || COALESCE(c->>'text', '');
                    WHEN 'mention' THEN line := line || '@' || COALESCE(c->'attrs'->>'label', '');
                    WHEN 'hardBreak' THEN line := line || E'\n';
                    ELSE NULL;
                    END CASE;
                END LOOP;
            END IF;
            IF line <> '' THEN
                out := out || line || E'\n';
            END IF;
        WHEN 'tableRow' THEN
            cells := '{}';
            IF jsonb_typeof(n->'content') = 'array' THEN
                FOR cell IN SELECT value FROM jsonb_array_elements(n->'content') LOOP
                    cells := cells || replace(btrim(requirement_plain_blocks(cell->'content', depth + 1), E' \t\n\r'), E'\n', ' ');
                END LOOP;
            END IF;
            out := out || array_to_string(cells, E'\t') || E'\n';
        ELSE
            out := out || requirement_plain_blocks(n->'content', depth + 1);
        END CASE;
    END LOOP;
    RETURN out;
END;
$$;
-- +goose StatementEnd

CREATE FUNCTION requirement_plain_text(body jsonb) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    RETURN btrim(requirement_plain_blocks(body->'content', 0), E' \t\n\r');

-- The words of a requirement's values. A generated column cannot ask an attribute its kind, so a value is read
-- by its shape: a document is rich text, and a string is text unless it is an id or a day, which select, person
-- and date values are. Numbers, yes or no and multi-selects say nothing a search looks for.
-- +goose StatementBegin
CREATE FUNCTION requirement_attribute_text(attrs jsonb) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT COALESCE(string_agg(
        CASE jsonb_typeof(v.value)
        WHEN 'object' THEN requirement_plain_text(v.value)
        WHEN 'string' THEN CASE WHEN v.value #>> '{}' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                                  OR v.value #>> '{}' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN NULL ELSE v.value #>> '{}' END
        END, E'\n' ORDER BY v.key), '')
    FROM jsonb_each(CASE WHEN jsonb_typeof(attrs) = 'object' THEN attrs ELSE '{}'::jsonb END) AS v
$$;
-- +goose StatementEnd

-- A tsvector holds at most 1 MB of lexemes, so each part is cut at 200000 characters, and what lies past that
-- is not found; positions past 16383 collapse in any case.
ALTER TABLE requirement ADD COLUMN search_vector tsvector GENERATED ALWAYS AS (
    setweight(to_tsvector('exciter_search'::regconfig, title), 'A') ||
    setweight(to_tsvector('exciter_search'::regconfig,
        left(requirement_plain_text(statement) || E'\n' || COALESCE(requirement_plain_text(rationale), ''), 200000)), 'B') ||
    setweight(to_tsvector('exciter_search'::regconfig, left(requirement_attribute_text(attributes), 200000)), 'C')
) STORED;

CREATE INDEX requirement_search_idx ON requirement USING gin (search_vector);

-- Search finds its words through the index before it checks who may see the hits, and checks a project at a time,
-- as Stator's 00700_search_hits.sql does for spaces. Under row level security the restrictive policy of
-- requirement runs perm_project_holds on every row of the organization first, because @@ is not leakproof and
-- so may not run before it. This reads as its owner, which row level security does not hold, then applies what
-- the policies apply to the matching rows only, out of the trash: it returns no row the caller could not read.
-- A key typed whole (project and number) is a hit whatever its words.
-- +goose StatementBegin
CREATE FUNCTION requirement_search_rows(q tsquery, key_project text, key_number integer)
    RETURNS TABLE (id uuid, org_id uuid, project_id uuid, number integer, kind text, title text, statement jsonb,
                   rationale jsonb, revision integer, status_id uuid, owner_id uuid, updated_at timestamptz,
                   updated_by uuid, rank real, key_match boolean)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH keyed AS MATERIALIZED (
        SELECT r.id FROM requirement r JOIN project p ON p.org_id = r.org_id AND p.id = r.project_id
        WHERE r.org_id = current_org_id() AND p.key = key_project AND r.number = key_number
    ), hit AS MATERIALIZED (
        SELECT r.id, r.project_id, r.number, r.kind, r.title, r.statement, r.rationale, r.revision, r.status_id, r.owner_id,
               r.updated_at, r.updated_by, ts_rank(r.search_vector, q) AS rank, r.id IN (SELECT k.id FROM keyed k) AS key_match
        FROM requirement r
        WHERE r.org_id = current_org_id() AND r.trashed_at IS NULL
          AND (r.search_vector @@ q OR r.id IN (SELECT k.id FROM keyed k))
    ), judged AS MATERIALIZED (
        SELECT j.project_id, perm_project_holds(j.project_id, current_actor_id(), 'view') AS holds
        FROM (SELECT DISTINCT h.project_id FROM hit h) j
    )
    SELECT h.id, current_org_id(), h.project_id, h.number, h.kind, h.title, h.statement, h.rationale, h.revision,
           h.status_id, h.owner_id, h.updated_at, h.updated_by, h.rank, h.key_match
    FROM hit h JOIN judged j ON j.project_id = h.project_id
    WHERE j.holds
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION requirement_search_rows(tsquery, text, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION requirement_search_rows(tsquery, text, integer) TO exciter_app, exciter_admin;

-- +goose Down
DROP FUNCTION IF EXISTS requirement_search_rows(tsquery, text, integer);
DROP INDEX IF EXISTS requirement_search_idx;
ALTER TABLE requirement DROP COLUMN IF EXISTS search_vector;
DROP FUNCTION IF EXISTS requirement_attribute_text(jsonb);
DROP FUNCTION IF EXISTS requirement_plain_text(jsonb);
DROP FUNCTION IF EXISTS requirement_plain_blocks(jsonb, integer);
DROP TEXT SEARCH CONFIGURATION IF EXISTS exciter_search;
