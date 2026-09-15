-- +goose Up
ALTER TABLE running_jobs ADD COLUMN retry_goal INTEGER;

-- +goose Down
ALTER TABLE running_jobs DROP COLUMN retry_goal;
