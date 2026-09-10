package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

var database *sql.DB

func beforeEach(t *testing.T) {
	database = setupTestDB(t)
}

func TestOpenDatabase_Success(t *testing.T) {
	db, err := OpenDatabase(":memory:")
	assert.NoError(t, err)
	assert.NotNil(t, db)

	assert.NoError(t, db.Close())
}

func TestOpenDatabase_CreatesExpectedSchema(t *testing.T) {
	db, err := OpenDatabase(":memory:")
	assert.NoError(t, err)
	assert.NotNil(t, db)

	var exists bool
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='running_jobs'").Scan(&exists)
	assert.NoError(t, err)
	assert.True(t, exists)

	assert.NoError(t, db.Close())
}

func TestNewDatabase_CreatesFileAndSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plumber.db")

	db, err := NewDatabase(path)
	assert.NoError(t, err)
	assert.NotNil(t, db)

	_, err = os.Stat(path)
	assert.NoError(t, err)

	var exists bool
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='running_jobs'").Scan(&exists)
	assert.NoError(t, err)
	assert.True(t, exists)

	assert.NoError(t, db.Close())
}

func TestNewDatabase_UsesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")

	f, err := os.Create(path)
	assert.NoError(t, err)
	assert.NoError(t, f.Close())

	db, err := NewDatabase(path)
	assert.NoError(t, err)
	assert.NotNil(t, db)
	assert.NoError(t, db.Close())
}

func TestIsRunningJob_ReturnsTrue(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, database, "job_key", 1, 42)

	assert.True(t, IsRunningJob(database, "job_key"))
}

func TestIsRunningJob_ReturnsFalse(t *testing.T) {
	beforeEach(t)

	assert.False(t, IsRunningJob(database, "missing_key"))
}

func TestIsRunningJob_ReturnsError(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, database, "job_key", 1, 42)

	err := database.Close()
	if err != nil {
		t.Fatal(err)
	}

	assert.False(t, IsRunningJob(database, "job_key"))
}

func TestDeleteJob_Success(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, database, "job_key", 1, 42)

	assert.True(t, IsRunningJob(database, "job_key"))
	err := DeleteJob(database, "job_key")

	assert.NoError(t, err)
	assert.False(t, IsRunningJob(database, "job_key"))
}

func TestDeleteJob_JobDoesNotExist(t *testing.T) {
	beforeEach(t)

	assert.False(t, IsRunningJob(database, "missing_key"))
	err := DeleteJob(database, "missing_key")

	assert.NoError(t, err)
	assert.False(t, IsRunningJob(database, "missing_key"))
}

func TestDeleteJob_ReturnsError(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, database, "job_key", 1, 42)

	err := database.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = DeleteJob(database, "job_key")
	assert.Error(t, err)
}

func TestGetRetryCount_ReturnsStoredValue(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, database, "job_key", 2, 42)

	count, err := GetRetryCount(database, "job_key")

	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestGetRetryCount_JobDoesNotExist(t *testing.T) {
	beforeEach(t)

	_, err := GetRetryCount(database, "missing_key")
	assert.Error(t, err)
}

func TestGetMergeRequestIid_ReturnsStoredValue(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, database, "job_key", 1, 99)

	iid, err := GetMergeRequestIid(database, "job_key")

	assert.NoError(t, err)
	assert.Equal(t, int64(99), iid)
}

func TestGetMergeRequestIid_JobDoesNotExist(t *testing.T) {
	beforeEach(t)

	_, err := GetMergeRequestIid(database, "missing_key")
	assert.Error(t, err)
}
