-- +goose Up
-- When the partner last refused or did not answer the refresh of a grant from a sign-in (#349), so reads and the
-- link worker wait a while before asking again rather than on every use. A refresh that works clears it. Only the
-- service writes it, as the admin role, which owns the tokens' other refresh columns too.
ALTER TABLE armature_token ADD COLUMN refresh_failed_at timestamptz;
ALTER TABLE stator_token ADD COLUMN refresh_failed_at timestamptz;

-- +goose Down
ALTER TABLE stator_token DROP COLUMN refresh_failed_at;
ALTER TABLE armature_token DROP COLUMN refresh_failed_at;
