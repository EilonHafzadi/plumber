package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func insertRunningJob(t *testing.T, key string, retryCount int, mergeRequestId int64) {
	t.Helper()

	_, err := db.Exec(
		"INSERT INTO running_jobs (key, retry_count, merge_request_id) VALUES (?, ?, ?)",
		key, retryCount, mergeRequestId,
	)

	if err != nil {
		t.Fatal(err)
	}

}

func TestCanRetry_NoExistingRow_ReturnsTrue(t *testing.T) {
	beforeEach(t)

	ok, err := canRetry("some_key")

	assert.NoError(t, err)
	assert.True(t, ok)
}

func TestCanRetry_ExistingRow_ReturnsFalse(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, "some_key", 1, 42)

	ok, err := canRetry("some_key")

	assert.NoError(t, err)
	assert.False(t, ok)
}

func TestCanRetry_ReturnsError(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, "some_key", 1, 42)

	err := db.Close()
	if err != nil {
		t.Fatal(err)
	}

	ok, err := canRetry("some_key")

	assert.Error(t, err)
	assert.False(t, ok)
}

func TestIsPlumberJob_ExistingRow_ReturnsTrue(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, "job_key", 1, 42)

	assert.True(t, isPlumberJob("job_key"))
}

func TestIsPlumberJob_NoExistingRow_ReturnsFalse(t *testing.T) {
	beforeEach(t)

	assert.False(t, isPlumberJob("missing_key"))
}

func TestIsPlumberJob_ReturnsError(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, "job_key", 1, 42)

	err := db.Close()
	if err != nil {
		t.Fatal(err)
	}

	assert.False(t, isPlumberJob("job_key"))
}

func TestDeleteJob_RemovesExistingRow(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, "job_key", 1, 42)

	err := deleteJob("job_key")

	assert.NoError(t, err)
	assert.False(t, isPlumberJob("job_key"))
}

func TestDeleteJob_NoExistingRow_NoError(t *testing.T) {
	beforeEach(t)

	err := deleteJob("missing_key")

	assert.NoError(t, err)
	assert.False(t, isPlumberJob("missing_key"))
}

func TestDeleteJob_DBError_ReturnsError(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, "job_key", 1, 42)

	err := db.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = deleteJob("job_key")

	assert.Error(t, err)
}

func TestGetRetryCount_ReturnsStoredValue(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, "job_key", 2, 42)

	count, err := getRetryCount("job_key")

	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestGetRetryCount_NoExistingRow_ReturnsError(t *testing.T) {
	beforeEach(t)

	_, err := getRetryCount("missing_key")

	assert.Error(t, err)
}

func TestGetMergeRequestIid_ReturnsStoredValue(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, "job_key", 1, 99)

	iid, err := getMergeRequestIid("job_key")

	assert.NoError(t, err)
	assert.Equal(t, int64(99), iid)
}

func TestGetMergeRequestIid_NoExistingRow_ReturnsError(t *testing.T) {
	beforeEach(t)

	_, err := getMergeRequestIid("missing_key")

	assert.Error(t, err)
}
