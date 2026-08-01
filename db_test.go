package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPlumberJob_ReturnsTrue(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, "job_key", 1, 42)

	assert.True(t, isPlumberJob("job_key"))
}

func TestIsPlumberJob_ReturnsFalse(t *testing.T) {
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

func TestDeleteJob_Success(t *testing.T) {
	beforeEach(t)
	insertRunningJob(t, "job_key", 1, 42)

	assert.True(t, isPlumberJob("job_key"))
	err := deleteJob("job_key")

	assert.NoError(t, err)
	assert.False(t, isPlumberJob("job_key"))
}

func TestDeleteJob_JobDoesNotExist(t *testing.T) {
	beforeEach(t)

	assert.False(t, isPlumberJob("missing_key"))
	err := deleteJob("missing_key")

	assert.NoError(t, err)
	assert.False(t, isPlumberJob("missing_key"))
}

func TestDeleteJob_ReturnsError(t *testing.T) {
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

func TestGetRetryCount_JobDoesNotExist(t *testing.T) {
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

func TestGetMergeRequestIid_JobDoesNotExist(t *testing.T) {
	beforeEach(t)

	_, err := getMergeRequestIid("missing_key")
	assert.Error(t, err)
}
