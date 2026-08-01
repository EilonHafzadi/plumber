package main

import "database/sql"

var db *sql.DB

// todo check how headscale do SQL db init
func initDatabase(dataSrcName string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dataSrcName)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS running_jobs (key VARCHAR(50) PRIMARY KEY, retry_count INTEGER, merge_request_id INTEGER)")
	if err != nil {
		return nil, err
	}

	return db, nil
}

func isPlumberJob(jobKey string) bool {
	query := `SELECT EXISTS(SELECT 1 FROM running_jobs WHERE key = ?)`

	var exists bool

	err := db.QueryRow(query, jobKey).Scan(&exists)
	if err != nil {
		return false
	}

	return exists
}

func deleteJob(jobKey string) error {
	_, err := db.Exec("DELETE FROM running_jobs WHERE key = ?", jobKey)

	if err != nil {
		return err
	}

	return nil
}

func getRetryCount(jobKey string) (int, error) {
	var retryCount int
	err := db.QueryRow("SELECT retry_count FROM running_jobs WHERE key = ?", jobKey).Scan(&retryCount)

	if err != nil {
		return -1, err
	}

	return retryCount, nil
}

func getMergeRequestIid(jobKey string) (int64, error) {
	var mergeRequestId int64
	err := db.QueryRow("SELECT merge_request_id FROM running_jobs WHERE key = ?", jobKey).Scan(&mergeRequestId)

	if err != nil {
		return -1, err
	}

	return mergeRequestId, nil
}
