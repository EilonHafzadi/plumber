package db

import (
	"database/sql"
	"embed"
	"os"

	"context"

	"github.com/pressly/goose/v3"
)

//go:embed migrations
var embedMigrations embed.FS

func OpenDatabase(dataSrcName string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dataSrcName)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS running_jobs (key VARCHAR(50) PRIMARY KEY, retry_count INTEGER, merge_request_id INTEGER)")
	if err != nil {
		return nil, err
	}

	goose.SetBaseFS(embedMigrations)

	err = goose.SetDialect("sqlite3")
	if err != nil {
		return nil, err
	}

	err = goose.UpContext(context.Background(), db, "migrations")
	if err != nil {
		return nil, err
	}

	return db, nil
}

// todo check how headscale do SQL db init
func NewDatabase(dataSrcName string) (*sql.DB, error) {
	_, err := os.Stat(dataSrcName)

	// db file does not exist
	if err != nil {
		_, err := os.Create(dataSrcName)
		if err != nil {
			return nil, err
		}

	}

	db, err := OpenDatabase(dataSrcName)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func IsRunningJob(db *sql.DB, jobKey string) bool {
	query := `SELECT EXISTS(SELECT 1 FROM running_jobs WHERE key = ?)`

	var exists bool

	err := db.QueryRow(query, jobKey).Scan(&exists)
	if err != nil {
		return false
	}

	return exists
}

func SetNoteID(db *sql.DB, jobKey string, noteID int64) error {
	_, err := db.Exec("UPDATE running_jobs SET note_id = ? WHERE key = ?", noteID, jobKey)
	return err
}

func DeleteJob(db *sql.DB, jobKey string) error {
	_, err := db.Exec("DELETE FROM running_jobs WHERE key = ?", jobKey)

	if err != nil {
		return err
	}

	return nil
}

func GetRetryCount(db *sql.DB, jobKey string) (int, error) {
	var retryCount int
	err := db.QueryRow("SELECT retry_count FROM running_jobs WHERE key = ?", jobKey).Scan(&retryCount)

	if err != nil {
		return -1, err
	}

	return retryCount, nil
}

func GetRetryGoal(db *sql.DB, jobKey string) (int, error) {
	var retryGoal int
	err := db.QueryRow("SELECT retry_goal FROM running_jobs WHERE key = ?", jobKey).Scan(&retryGoal)

	if err != nil {
		return -1, err
	}

	return retryGoal, nil
}

func GetMergeRequestIid(db *sql.DB, jobKey string) (int64, error) {
	var mergeRequestId int64
	err := db.QueryRow("SELECT merge_request_id FROM running_jobs WHERE key = ?", jobKey).Scan(&mergeRequestId)

	if err != nil {
		return -1, err
	}

	return mergeRequestId, nil
}

func GetDiscussionId(db *sql.DB, jobKey string) (string, error) {
	var discussionId string
	err := db.QueryRow("SELECT discussion_id FROM running_jobs WHERE key = ?", jobKey).Scan(&discussionId)

	if err != nil {
		return "", err
	}

	return discussionId, nil
}

func GetNoteId(db *sql.DB, jobKey string) (int64, error) {
	var noteId int64
	err := db.QueryRow("SELECT note_id FROM running_jobs WHERE key = ?", jobKey).Scan(&noteId)

	if err != nil {
		return -1, err
	}

	return noteId, nil
}
