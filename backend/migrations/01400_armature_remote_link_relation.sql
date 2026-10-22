-- +goose Up
-- The relation a synced link was last sent with (#297), so a link synced before Armature took relations is sent
-- again once it does. Null is sent without one.
ALTER TABLE armature_remote_link ADD COLUMN relation text CHECK (relation IN ('page', 'requirement'));

-- +goose Down
ALTER TABLE armature_remote_link DROP COLUMN relation;
