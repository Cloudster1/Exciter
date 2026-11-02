-- +goose Up
-- The suite demo files hand-written specifications (#488): a requirement or test case is found again by the ID its
-- file gives it, which an issue number cannot say. The architecture's views keep issue 0 and their place.
ALTER TABLE suite_demo_requirement
    ADD COLUMN spec_id text NOT NULL DEFAULT '' CHECK (spec_id ~ '^[A-Z0-9-]{0,64}$'),
    DROP CONSTRAINT suite_demo_requirement_once,
    ADD CONSTRAINT suite_demo_requirement_once UNIQUE (project_id, issue, criterion, spec_id);

-- +goose Down
ALTER TABLE suite_demo_requirement
    DROP CONSTRAINT suite_demo_requirement_once,
    DROP COLUMN spec_id,
    ADD CONSTRAINT suite_demo_requirement_once UNIQUE (project_id, issue, criterion);
