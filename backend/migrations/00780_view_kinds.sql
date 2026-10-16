-- +goose Up
-- Views of a matrix and of a chain report beside the grid's (#152): a view has a kind, which it keeps for good, and
-- the settings of that kind, which the service checks before it saves them. A name is one owner's per project and
-- kind, so a grid and a matrix may share one.
ALTER TABLE saved_view ADD COLUMN kind text NOT NULL DEFAULT 'grid' CHECK (kind IN ('grid', 'matrix', 'chain'));
ALTER TABLE saved_view ADD COLUMN settings jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(settings) = 'object');

DROP INDEX saved_view_name_key;
CREATE UNIQUE INDEX saved_view_name_key ON saved_view (project_id, owner_id, kind, lower(name));

GRANT UPDATE (settings) ON saved_view TO exciter_app;

-- +goose Down
REVOKE UPDATE (settings) ON saved_view FROM exciter_app;
DROP INDEX saved_view_name_key;
DELETE FROM saved_view WHERE kind <> 'grid';
CREATE UNIQUE INDEX saved_view_name_key ON saved_view (project_id, owner_id, lower(name));
ALTER TABLE saved_view DROP COLUMN settings;
ALTER TABLE saved_view DROP COLUMN kind;
