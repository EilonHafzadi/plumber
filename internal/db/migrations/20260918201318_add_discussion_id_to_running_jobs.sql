-- +goose Up
ALTER TABLE running_jobs ADD COLUMN discussion_id TEXT DEFAULT "";

-- +goose Down
ALTER TABLE running_jobs DROP COLUMN discussion_id;
