-- +goose Up
ALTER TABLE running_jobs ADD COLUMN note_id BIGINT DEFAULT -1;

-- +goose Down
ALTER TABLE running_jobs DROP COLUMN note_id;
