package db

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func SetupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatal("failed to init database: " + err.Error())
	}
	t.Cleanup(func() {
		db.Close()
	})
	return db
}

func InsertRunningJob(t *testing.T, db *sql.DB, key string, retryCount int, mergeRequestId int64) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO running_jobs (key, retry_count, merge_request_id) VALUES (?, ?, ?)",
		key, retryCount, mergeRequestId,
	)
	if err != nil {
		t.Fatal(err)
	}
}
