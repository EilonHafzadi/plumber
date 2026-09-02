package db

import (
	"database/sql"
	"testing"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatal("failed to init database: " + err.Error())
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
	})
	return database
}

func insertRunningJob(t *testing.T, database *sql.DB, key string, retryCount int, mergeRequestIID int64) {
	t.Helper()
	if _, err := database.Exec(
		"INSERT INTO running_jobs (key, retry_count, merge_request_id) VALUES (?, ?, ?)",
		key, retryCount, mergeRequestIID,
	); err != nil {
		t.Fatal(err)
	}
}