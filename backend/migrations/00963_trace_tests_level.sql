-- +goose Up
-- A trace model may end in a level of tests (#200), reached by verifies from the requirements before it: the tests
-- whose latest results on the counting branch verify them.
ALTER TABLE trace_level DROP CONSTRAINT trace_level_kind_check,
    ADD CONSTRAINT trace_level_kind_check CHECK (kind IN ('requirements', 'issues', 'pages', 'tests'));

-- +goose Down
DELETE FROM trace_level WHERE kind = 'tests';
ALTER TABLE trace_level DROP CONSTRAINT trace_level_kind_check,
    ADD CONSTRAINT trace_level_kind_check CHECK (kind IN ('requirements', 'issues', 'pages'));
