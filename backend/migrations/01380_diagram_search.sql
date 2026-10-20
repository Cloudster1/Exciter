-- +goose Up
-- Search finds the words of a diagram (#284): its labels are in its source, as document.PlainText reads it. No row
-- held a diagram before this, so no stored search vector needs computing again.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION requirement_plain_blocks(blocks jsonb, depth integer) RETURNS text
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
        WHEN 'diagram' THEN
            IF COALESCE(n->'attrs'->>'source', '') <> '' THEN
                out := out || (n->'attrs'->>'source') || E'\n';
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

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION requirement_plain_blocks(blocks jsonb, depth integer) RETURNS text
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
