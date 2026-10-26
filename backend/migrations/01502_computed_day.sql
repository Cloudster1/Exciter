-- +goose Up
-- today() moves on at midnight without any change to mark what reads it (#360). The worker turns each project that
-- calls it to the new day once, marking only what those formulas show on; the day kept is also the earliest one a
-- recompute of the project uses, so a worker whose clock is a little behind does not work yesterday out again.
CREATE TABLE computed_day (
    project_id uuid PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    day        date NOT NULL
);

ALTER TABLE computed_day ENABLE ROW LEVEL SECURITY;
ALTER TABLE computed_day FORCE  ROW LEVEL SECURITY;
CREATE POLICY computed_day_installation ON computed_day USING (true) WITH CHECK (true);
GRANT SELECT, INSERT, UPDATE, DELETE ON computed_day TO exciter_admin;
REVOKE ALL ON computed_day FROM exciter_app;

-- +goose Down
DROP TABLE IF EXISTS computed_day;
