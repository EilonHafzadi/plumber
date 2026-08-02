package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
	gitlabtesting "gitlab.com/gitlab-org/api/client-go/v2/testing"
	"go.uber.org/mock/gomock"
)

// todo improve beforeEach usage
func beforeEach(t *testing.T) {
	setupLogger(t)
	setupSettings(t)

	database, err := initDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	db = database
}

func TestBotMention_ExactMatch_Triggers(t *testing.T) {
	beforeEach(t)

	opts := &gitlab.ListJobsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
	}

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), opts).
		Return([]*gitlab.Job{{ID: 1, Name: settings.JobName}}, &gitlab.Response{}, nil)

	client.MockJobs.EXPECT().
		RetryJob(83, int64(1)).
		Return(&gitlab.Job{ID: 1}, &gitlab.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s retry", settings.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := sendRequest(client, request)
	if rec.Code != http.StatusCreated {
		t.Errorf("expected %d but got %d", http.StatusOK, rec.Code)
	}

}

func TestProcessWebhook_InvalidObjectKind_DoesNotRetry(t *testing.T) {
	beforeEach(t)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	payload := commentPayload("push", fmt.Sprintf("@%s retry", settings.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected %d but got %d", http.StatusBadRequest, rec.Code)
	}

	assertNextMessage(t, "unsupported webhook type\n", rec)
}

func TestProcessWebhook_InvalidPayload_DoesNotRetry(t *testing.T) {
	beforeEach(t)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	request := httptest.NewRequest(http.MethodPost, "/webhook", iotest.ErrReader(errors.New("boom")))
	recorder := sendRequest(client, request)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected %d but got %d", http.StatusBadRequest, recorder.Code)
	}

	assertNextMessage(t, "failed to read body\n", recorder)
}

func TestProcessWebhook_InvalidJson_DoesNotRetry(t *testing.T) {
	beforeEach(t)

	payload := `{
		"object_kind": "` + "note" + `",
		"project_id": 83,
		"object_attributes": {
			"note": "` + "hello world" + `",
			"noteable_type": "MergeRequest"
		},
		"merge_request": {
			"head_pipeline_id": ` + fmt.Sprintf("%d", 1) + `
	}`

	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))
	recorder := sendRequest(client, request)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected %d but got %d", http.StatusBadRequest, recorder.Code)
	}

	assertNextMessage(t, "failed to decode webhook\n", recorder)
}

func TestProcessWebhook_InvalidCommentWebhook_DoesNotRetry(t *testing.T) {
	beforeEach(t)

	payload := `{
		"object_kind": "note",
		"project_id": 83,
		"object_attributes": "not an object"
	}`

	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))
	recorder := sendRequest(client, request)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected %d but got %d", http.StatusBadRequest, recorder.Code)
	}

	assertNextMessage(t, "failed to decode comment webhook\n", recorder)
}

func TestProcessWebhook_CommentNotMRComment_DoesNotRetry(t *testing.T) {
	beforeEach(t)

	payload := `{
		"object_kind": "note",
		"project_id": 83,
		"object_attributes": {
		"noteable-type": "Issue"
		}
	}`

	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))
	recorder := sendRequest(client, request)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected %d but got %d", http.StatusBadRequest, recorder.Code)
	}

	assertNextMessage(t, "comment is not merge request comment\n", recorder)
}

func TestProcessWebhook_ListPipelineJobs_Fails_DoesNotRetry(t *testing.T) {
	beforeEach(t)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	notFoundResp := &gitlab.Response{
		Response: &http.Response{StatusCode: http.StatusNotFound},
	}

	client.MockJobs.EXPECT().
		ListPipelineJobs(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, notFoundResp, errors.New("404 Not Found"))

	payload := commentPayload("note", fmt.Sprintf("@%s retry", settings.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := sendRequest(client, request)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected %d but got %d", http.StatusInternalServerError, rec.Code)
	}

	assertNextMessage(t, "failed to retrieve job id: 404 Not Found\n", rec)
}

func TestProcessWebhook_GetJobId_ReturnsEmpty_DoesNotRetry(t *testing.T) {
	beforeEach(t)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	opts := &gitlab.ListJobsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), opts).
		Return([]*gitlab.Job{}, &gitlab.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s retry", settings.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := sendRequest(client, request)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected %d but got %d", http.StatusInternalServerError, rec.Code)
	}

	assertNextMessage(t, "failed to retrieve job id: no jobs found for pipeline 1\n", rec)
}

func TestProcessWebhook_GetJobId_FailsJobNotFound_DoesNotRetry(t *testing.T) {
	beforeEach(t)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	opts := &gitlab.ListJobsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), opts).
		Return([]*gitlab.Job{{ID: 2, Name: "other_job"}}, &gitlab.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s retry", settings.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := sendRequest(client, request)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected %d but got %d", http.StatusInternalServerError, rec.Code)
	}

	assertNextMessage(t, "failed to retrieve job id: no job with name "+settings.JobName+"\n", rec)
}

func TestProcessWebhook_RetryJob_Fails_DoesNotRetry(t *testing.T) {
	beforeEach(t)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	opts := &gitlab.ListJobsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), opts).
		Return([]*gitlab.Job{{ID: 1, Name: settings.JobName}}, &gitlab.Response{}, nil)

	client.MockJobs.EXPECT().
		RetryJob(gomock.Any(), gomock.Any()).
		Return(nil, &gitlab.Response{}, errors.New("oh no rip"))

	payload := commentPayload("note", fmt.Sprintf("@%s retry", settings.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := sendRequest(client, request)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected %d but got %d", http.StatusInternalServerError, rec.Code)
	}

	assertNextMessage(t, "failed to retry job: oh no rip\n", rec)
}

func TestProcessWebhook_Build_InvalidJson_DoesNotProcess(t *testing.T) {
	beforeEach(t)

	payload := `{
		"object_kind": "build",
		"project_id": 83,
		"build_id": "not-a-number",
		"build_status": "success",
		"pipeline_id": 10,
		"build_name": "job_test"
	}`

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 400, rec.Code)
	assertNextMessage(t, "failed to decode job webhook\n", rec)
}

func TestProcessWebhook_Build_NotPlumberJob_DoesNothing(t *testing.T) {
	beforeEach(t)

	// No row seeded in running_jobs, so this build is not one plumber started.
	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	payload := jobPayload("success", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, 0, countRunningJobs(t, buildJobKey(83, 10)))
}

func TestProcessWebhook_Build_StatusNotFinal_DoesNothing(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, 0, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	// "running" is neither "success" nor "failed" so nothing should happen.
	payload := jobPayload("running", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	retryCount, err := getRetryCount(jobKey)
	assert.NoError(t, err)
	assert.Equal(t, 0, retryCount)
}

func TestProcessWebhook_Build_Success_Retries_Again(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, 0, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockJobs.EXPECT().
		RetryJob(83, int64(55)).
		Return(&gitlab.Job{ID: 55}, &gitlab.Response{}, nil)

	payload := jobPayload("success", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	retryCount, err := getRetryCount(jobKey)
	assert.NoError(t, err)
	assert.Equal(t, 1, retryCount)

	assert.True(t, isRunningJob(jobKey))
}

func TestProcessWebhook_Build_Success_ApprovesAndStopsTracking(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, settings.RetryAmount, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlab.MergeRequestApprovals{UserHasApproved: false}, &gitlab.Response{}, nil)

	client.MockMergeRequestApprovals.EXPECT().
		ApproveMergeRequest(83, int64(7), nil).
		Return(&gitlab.MergeRequestApprovals{UserHasApproved: true}, &gitlab.Response{}, nil)

	payload := jobPayload("success", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	// job is finished: no longer tracked as a running plumber job
	assert.False(t, isRunningJob(jobKey))
}

func TestProcessWebhook_Build_Success_MR_AlreadyApproved_DoesNotReapprove(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, settings.RetryAmount, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlab.MergeRequestApprovals{UserHasApproved: true}, &gitlab.Response{}, nil)

	// No ApproveMergeRequest expectation set: it must not be called again.

	payload := jobPayload("success", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.False(t, isRunningJob(jobKey))
}

func TestProcessWebhook_Build_Failed_UnapprovesMergeRequest(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, 1, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlab.MergeRequestApprovals{UserHasApproved: true}, &gitlab.Response{}, nil)

	client.MockMergeRequestApprovals.EXPECT().
		UnapproveMergeRequest(83, int64(7)).
		Return(&gitlab.Response{}, nil)

	payload := jobPayload("failed", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.False(t, isRunningJob(jobKey))
}

func TestProcessWebhook_Build_Failed_AlreadyUnapproved_DoesNotReUnapprove(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, 1, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlab.MergeRequestApprovals{UserHasApproved: false}, &gitlab.Response{}, nil)

	payload := jobPayload("failed", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
}

func TestProcessWebhook_Build_Success_RetryJobFails_DoesNotIncrementCount(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, 0, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockJobs.EXPECT().
		RetryJob(83, int64(55)).
		Return(nil, &gitlab.Response{}, errors.New("gitlab is down"))

	payload := jobPayload("success", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	// handleJobWebhook doesn't write an HTTP error for this failure - it just
	// logs and returns - so the response is still 200.
	assert.Equal(t, 200, rec.Code)

	// retry_count must be unchanged since the update only happens after a
	// successful retry
	retryCount, err := getRetryCount(jobKey)
	assert.NoError(t, err)
	assert.Equal(t, 0, retryCount)
	assert.True(t, isRunningJob(jobKey))
}

func TestProcessWebhook_Build_Success_GetConfigurationFails_JobStillDeleted(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, settings.RetryAmount, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(nil, &gitlab.Response{}, errors.New("network error"))

	payload := jobPayload("success", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.False(t, isRunningJob(jobKey))
}

func TestProcessWebhook_Build_Success_ApproveMergeRequestFails_JobStillDeleted(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, settings.RetryAmount, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlab.MergeRequestApprovals{UserHasApproved: false}, &gitlab.Response{}, nil)

	client.MockMergeRequestApprovals.EXPECT().
		ApproveMergeRequest(83, int64(7), nil).
		Return(nil, &gitlab.Response{}, errors.New("approval rejected"))

	payload := jobPayload("success", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	// same gap as above: the row is gone from running_jobs even though the
	// approve call itself failed
	assert.False(t, isRunningJob(jobKey))
}

func TestProcessWebhook_Build_Failed_GetConfigurationFails_NoStateChange(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, 1, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(nil, &gitlab.Response{}, errors.New("network error"))

	payload := jobPayload("failed", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.False(t, isRunningJob(jobKey))
}

func TestProcessWebhook_Build_Failed_UnapproveMergeRequestFails_NoStateChange(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, 1, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlab.MergeRequestApprovals{UserHasApproved: true}, &gitlab.Response{}, nil)

	client.MockMergeRequestApprovals.EXPECT().
		UnapproveMergeRequest(83, int64(7)).
		Return(&gitlab.Response{}, errors.New("network error"))

	payload := jobPayload("failed", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.False(t, isRunningJob(jobKey))
}

func TestProcessWebhook_OnRetryCommand_isRunningJob_ReturnsFalse(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	opts := &gitlab.ListJobsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().ListPipelineJobs(83, int64(10), opts).
		Return([]*gitlab.Job{{ID: 1, Name: settings.JobName}}, &gitlab.Response{}, nil)

	client.MockJobs.EXPECT().
		RetryJob(83, int64(1)).
		Return(&gitlab.Job{ID: 1}, &gitlab.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s", settings.RetryCommand), 10)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))

	assert.False(t, isRunningJob(jobKey))
	rec := sendRequest(client, request)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.True(t, isRunningJob(jobKey))
}

func TestProcessWebhook_OnRetryCommand_isRunningJob_ReturnsTrue(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, 1, 7)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	opts := &gitlab.ListJobsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().ListPipelineJobs(83, int64(10), opts).
		Return([]*gitlab.Job{{ID: 1, Name: settings.JobName}}, &gitlab.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s", settings.RetryCommand), 10)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))

	// before requesting a retry we ensure this job is already running
	assert.True(t, isRunningJob(jobKey))
	rec := sendRequest(client, request)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, isRunningJob(jobKey))
}

func TestProcessWebhook_OnRetryCommand_InsertFails(t *testing.T) {
	beforeEach(t)
	forceReadOnly(t)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	opts := &gitlab.ListJobsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), opts).
		Return([]*gitlab.Job{{ID: 1, Name: settings.JobName}}, &gitlab.Response{}, nil)

	client.MockJobs.EXPECT().
		RetryJob(83, int64(1)).
		Return(&gitlab.Job{ID: 1}, &gitlab.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s retry", settings.RetryCommand), 1)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	jobKey := fmt.Sprintf("%d_%d_%s", 83, 1, settings.JobName)
	assert.Equal(t, 0, countRunningJobs(t, jobKey))
}

func TestProcessWebhook_HandleJobWebhook_UpdateFails(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)

	insertRunningJob(t, jobKey, 0, 7)
	forceReadOnly(t)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockJobs.EXPECT().
		RetryJob(83, int64(55)).
		Return(&gitlab.Job{ID: 55}, &gitlab.Response{}, nil)

	payload := jobPayload("success", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	retryCount, err := getRetryCount(jobKey)
	assert.NoError(t, err)
	assert.Equal(t, 0, retryCount)

	assert.True(t, isRunningJob(jobKey))
}

func TestProcessWebhook_HandleJobWebhook_GetRetryCountFails(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)

	insertRunningJob(t, jobKey, 0, 7)
	updateRetryCount(t, jobKey, "not-a-number")

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	payload := jobPayload("success", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
}

func TestProcessWebhook_HandleJobWebhook_GetMergeRequestIid_Fails(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)

	// retry_count is valid so getRetryCount succeeds; merge_request_id is
	// corrupted so the very next call, getMergeRequestIid, fails.
	insertRunningJob(t, jobKey, 1, 7)
	updateMergeRequestIid(t, jobKey, "not-a-number")

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	payload := jobPayload("success", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
}

func TestProcessWebhook_OnJobInProgress_GetMergeRequestIid_Fails(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, 0, 7)
	updateMergeRequestIid(t, jobKey, "not-a-number")

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockJobs.EXPECT().
		RetryJob(83, int64(55)).
		Return(&gitlab.Job{ID: 55}, &gitlab.Response{}, nil)

	jobWebhook := &JobWebhook{
		GitlabWebhook: GitlabWebhook{ObjectKind: "build", ProjectId: 83},
		Id:            55,
		Status:        "success",
		ProjectId:     83,
		PipelineId:    10,
		Name:          settings.JobName,
	}

	onJobInProgress(client.Client, jobWebhook, jobKey, 0)

	retryCount, err := getRetryCount(jobKey)
	assert.NoError(t, err)
	assert.Equal(t, 0, retryCount)
}

func TestProcessWebhook_OnJobFinished_DeleteJobFails(t *testing.T) {
	beforeEach(t)
	jobKey := buildJobKey(83, 10)
	insertRunningJob(t, jobKey, settings.RetryAmount, 7)
	forceReadOnly(t)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	payload := jobPayload("success", 83, 10, 55)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.True(t, isRunningJob(jobKey))
}
