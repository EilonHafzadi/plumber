package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	gitlabtesting "gitlab.com/gitlab-org/api/client-go/v2/testing"
)

func setupSettings(t *testing.T) {
	err := initSettings(".")
	if err != nil {
		t.Fatal("failed to init settings: " + err.Error())
	}

}

func setupLogger(t *testing.T) {
	err := initLogger()
	if err != nil {
		t.Fatal("failed to init logger: " + err.Error())
	}

}

func commentPayload(objectKind string, note string, headPipelineId int) string {
	return `{
		"object_kind": "` + objectKind + `",
		"project_id": 83,
		"object_attributes": {
			"note": "` + note + `",
			"noteable_type": "MergeRequest"
		},
		"merge_request": {
			"head_pipeline_id": ` + fmt.Sprintf("%d", headPipelineId) + `
		}
	}`
}

func jobPayload(status string, projectId int, pipelineId int, buildId int64) string {
	return fmt.Sprintf(`{
		"object_kind": "build",
		"project_id": %d,
		"build_id": %d,
		"build_status": "%s",
		"pipeline_id": %d,
		"build_name": "%s"
	}`, projectId, buildId, status, pipelineId, settings.JobName)
}

func countRunningJobs(t *testing.T, key string) int {
	t.Helper()

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM running_jobs WHERE key = ?", key).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	return count
}

func buildJobKey(projectId, pipelineId int) string {
	return fmt.Sprintf("%d_%d_%s", projectId, pipelineId, settings.JobName)
}

func sendRequest(client *gitlabtesting.TestClient, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	processWebhook(client.Client, recorder, request)
	return recorder
}

func assertNextMessage(t *testing.T, expected string, recorder *httptest.ResponseRecorder) {
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}

	msg := string(body)
	assert.Equal(t, expected, msg)
}

func forceReadOnly(t *testing.T) {
	t.Helper()
	_, err := db.Exec("PRAGMA query_only = ON")
	if err != nil {
		t.Fatal(err)
	}
}

func updateRetryCount(t *testing.T, key string, value string) {
	t.Helper()

	_, err := db.Exec("UPDATE running_jobs SET retry_count = ? WHERE key = ?", value, key)
	if err != nil {
		t.Fatal(err)
	}
}

func updateMergeRequestIid(t *testing.T, key string, value string) {
	t.Helper()

	_, err := db.Exec("UPDATE running_jobs SET merge_request_id = ? WHERE key = ?", value, key)
	if err != nil {
		t.Fatal(err)
	}
}

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
