-- +goose Up
-- PDF export (#45), as Stator's (#86 there). The render service opens the print view as its reader with a personal
-- access token made for that one print: read only, gone within minutes, listed nowhere and deleted once the PDF is
-- back. The api's own code makes them so; this check holds a raw INSERT or UPDATE as the app role to the same.
ALTER TABLE api_token ADD COLUMN for_render boolean NOT NULL DEFAULT false;

ALTER TABLE api_token ADD CONSTRAINT api_token_render_is_brief CHECK (
    NOT for_render
    OR (scopes = ARRAY['read']::text[]
        AND NOT projects_only
        AND expires_at IS NOT NULL
        AND expires_at <= created_at + interval '5 minutes')
);

-- +goose Down
ALTER TABLE api_token DROP CONSTRAINT IF EXISTS api_token_render_is_brief;
ALTER TABLE api_token DROP COLUMN IF EXISTS for_render;
