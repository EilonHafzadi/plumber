package gitlab_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"plumber/internal/db"
	"plumber/internal/gitlab"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	gitlabapi "gitlab.com/gitlab-org/api/client-go/v2"
	gitlabtesting "gitlab.com/gitlab-org/api/client-go/v2/testing"
	"go.uber.org/mock/gomock"
)

func TestBotMention_ExactMatch_Triggers(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	opts := &gitlabapi.ListJobsOptions{
		ListOptions: gitlabapi.ListOptions{
			PerPage: 100,
		},
	}

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), opts).
		Return([]*gitlabapi.Job{{ID: 1, Name: fixture.Cfg.JobName}}, &gitlabapi.Response{}, nil)

	client.MockJobs.EXPECT().
		RetryJob(83, int64(1)).
		Return(&gitlabapi.Job{ID: 1}, &gitlabapi.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s retry", fixture.Cfg.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := fixture.sendRequest(client, request)
	if rec.Code != http.StatusCreated {
		t.Errorf("expected %d but got %d", http.StatusOK, rec.Code)
	}

}

func TestProcessWebhook_InvalidObjectKind_DoesNotRetry(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	payload := commentPayload("push", fmt.Sprintf("@%s retry", fixture.Cfg.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected %d but got %d", http.StatusBadRequest, rec.Code)
	}

	assertNextMessage(t, "unsupported webhook type\n", rec)
}

func TestProcessWebhook_InvalidPayload_DoesNotRetry(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	request := httptest.NewRequest(http.MethodPost, "/webhook", iotest.ErrReader(errors.New("boom")))
	rec := fixture.sendRequest(client, request)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected %d but got %d", http.StatusBadRequest, rec.Code)
	}

	assertNextMessage(t, "failed to read body\n", rec)
}

func TestProcessWebhook_InvalidJson_DoesNotRetry(t *testing.T) {
	fixture := newWebhookTestFixture(t)

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

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))
	rec := fixture.sendRequest(client, request)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected %d but got %d", http.StatusBadRequest, rec.Code)
	}

	assertNextMessage(t, "failed to decode webhook\n", rec)
}

func TestProcessWebhook_InvalidCommentWebhook_DoesNotRetry(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	payload := `{
		"object_kind": "note",
		"project_id": 83,
		"object_attributes": "not an object"
	}`

	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))
	rec := fixture.sendRequest(client, request)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected %d but got %d", http.StatusBadRequest, rec.Code)
	}

	assertNextMessage(t, "failed to decode comment webhook\n", rec)
}

func TestProcessWebhook_CommentNotMRComment_DoesNotRetry(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	payload := `{
		"object_kind": "note",
		"project_id": 83,
		"object_attributes": {
		"noteable-type": "Issue"
		}
	}`

	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))
	rec := fixture.sendRequest(client, request)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected %d but got %d", http.StatusBadRequest, rec.Code)
	}

	assertNextMessage(t, "comment is not merge request comment\n", rec)
}

func TestProcessWebhook_ListPipelineJobs_Fails_DoesNotRetry(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	notFoundResp := &gitlabapi.Response{
		Response: &http.Response{StatusCode: http.StatusNotFound},
	}

	client.MockJobs.EXPECT().
		ListPipelineJobs(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, notFoundResp, errors.New("404 Not Found"))

	payload := commentPayload("note", fmt.Sprintf("@%s retry", fixture.Cfg.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := fixture.sendRequest(client, request)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected %d but got %d", http.StatusInternalServerError, rec.Code)
	}

	assertNextMessage(t, "failed to retrieve job id: 404 Not Found\n", rec)
}

func TestProcessWebhook_GetJobId_ReturnsEmpty_DoesNotRetry(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	opts := &gitlabapi.ListJobsOptions{
		ListOptions: gitlabapi.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), opts).
		Return([]*gitlabapi.Job{}, &gitlabapi.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s retry", fixture.Cfg.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := fixture.sendRequest(client, request)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected %d but got %d", http.StatusInternalServerError, rec.Code)
	}

	assertNextMessage(t, "failed to retrieve job id: no jobs found for pipeline 1\n", rec)
}

func TestProcessWebhook_GetJobId_FailsJobNotFound_DoesNotRetry(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	opts := &gitlabapi.ListJobsOptions{
		ListOptions: gitlabapi.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), opts).
		Return([]*gitlabapi.Job{{ID: 2, Name: "other_job"}}, &gitlabapi.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s retry", fixture.Cfg.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := fixture.sendRequest(client, request)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected %d but got %d", http.StatusInternalServerError, rec.Code)
	}

	assertNextMessage(t, "failed to retrieve job id: no job with name "+fixture.Cfg.JobName+"\n", rec)
}

func TestProcessWebhook_RetryJob_Fails_DoesNotRetry(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	opts := &gitlabapi.ListJobsOptions{
		ListOptions: gitlabapi.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), opts).
		Return([]*gitlabapi.Job{{ID: 1, Name: fixture.Cfg.JobName}}, &gitlabapi.Response{}, nil)

	client.MockJobs.EXPECT().
		RetryJob(gomock.Any(), gomock.Any()).
		Return(nil, &gitlabapi.Response{}, errors.New("oh no rip"))

	payload := commentPayload("note", fmt.Sprintf("@%s retry", fixture.Cfg.RetryCommand), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := fixture.sendRequest(client, request)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected %d but got %d", http.StatusInternalServerError, rec.Code)
	}

	assertNextMessage(t, "failed to retry job: oh no rip\n", rec)
}

func TestProcessWebhook_Build_InvalidJson_DoesNotProcess(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	payload := `{
		"object_kind": "build",
		"project_id": 83,
		"build_id": "not-a-number",
		"build_status": "success",
		"pipeline_id": 10,
		"build_name": "job_test"
	}`

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 400, rec.Code)
	assertNextMessage(t, "failed to decode job webhook\n", rec)
}

func TestProcessWebhook_Build_NotPlumberJob_DoesNothing(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	// No row seeded in running_jobs, so this build is not one plumber started.
	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	payload := jobPayload("success", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, 0, countRunningJobs(t, fixture.Database, buildJobKey(83, 10, fixture.Cfg.JobName)))
}

func TestProcessWebhook_Build_StatusNotFinal_DoesNothing(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, 0, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	// "running" is neither "success" nor "failed" so nothing should happen.
	payload := jobPayload("running", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	retryCount, err := db.GetRetryCount(fixture.Database, jobKey)
	assert.NoError(t, err)
	assert.Equal(t, 0, retryCount)
}

func TestProcessWebhook_Build_Success_Retries_Again(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, 0, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	client.MockJobs.EXPECT().
		RetryJob(83, int64(55)).
		Return(&gitlabapi.Job{ID: 55}, &gitlabapi.Response{}, nil)

	payload := jobPayload("success", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	retryCount, err := db.GetRetryCount(fixture.Database, jobKey)
	assert.NoError(t, err)
	assert.Equal(t, 1, retryCount)

	assert.True(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_Build_Success_ApprovesAndStopsTracking(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, fixture.Cfg.RetryAmount, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlabapi.MergeRequestApprovals{UserHasApproved: false}, &gitlabapi.Response{}, nil)

	client.MockMergeRequestApprovals.EXPECT().
		ApproveMergeRequest(83, int64(7), nil).
		Return(&gitlabapi.MergeRequestApprovals{UserHasApproved: true}, &gitlabapi.Response{}, nil)

	payload := jobPayload("success", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	// job is finished: no longer tracked as a running plumber job
	assert.False(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_Build_Success_MR_AlreadyApproved_DoesNotReapprove(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, fixture.Cfg.RetryAmount, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlabapi.MergeRequestApprovals{UserHasApproved: true}, &gitlabapi.Response{}, nil)

	// No ApproveMergeRequest expectation set: it must not be called again.

	payload := jobPayload("success", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.False(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_Build_Failed_UnapprovesMergeRequest(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, 1, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlabapi.MergeRequestApprovals{UserHasApproved: true}, &gitlabapi.Response{}, nil)

	client.MockMergeRequestApprovals.EXPECT().
		UnapproveMergeRequest(83, int64(7)).
		Return(&gitlabapi.Response{}, nil)

	payload := jobPayload("failed", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.False(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_Build_Failed_AlreadyUnapproved_DoesNotReUnapprove(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, 1, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlabapi.MergeRequestApprovals{UserHasApproved: false}, &gitlabapi.Response{}, nil)

	payload := jobPayload("failed", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
}

func TestProcessWebhook_Build_Success_RetryJobFails_DoesNotIncrementCount(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, 0, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	client.MockJobs.EXPECT().
		RetryJob(83, int64(55)).
		Return(nil, &gitlabapi.Response{}, errors.New("gitlab is down"))

	payload := jobPayload("success", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	// handleJobWebhook doesn't write an HTTP error for this failure - it just
	// logs and returns - so the response is still 200.
	assert.Equal(t, 200, rec.Code)

	// retry_count is rolled back when the retry request fails.
	retryCount, err := db.GetRetryCount(fixture.Database, jobKey)
	assert.NoError(t, err)
	assert.Equal(t, 0, retryCount)
	assert.True(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_Build_Success_GetConfigurationFails_JobStillDeleted(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, fixture.Cfg.RetryAmount, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(nil, &gitlabapi.Response{}, errors.New("network error"))

	payload := jobPayload("success", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.False(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_Build_Success_ApproveMergeRequestFails_JobStillDeleted(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, fixture.Cfg.RetryAmount, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlabapi.MergeRequestApprovals{UserHasApproved: false}, &gitlabapi.Response{}, nil)

	client.MockMergeRequestApprovals.EXPECT().
		ApproveMergeRequest(83, int64(7), nil).
		Return(nil, &gitlabapi.Response{}, errors.New("approval rejected"))

	payload := jobPayload("success", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	// same gap as above: the row is gone from running_jobs even though the
	// approve call itself failed
	assert.False(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_Build_Failed_GetConfigurationFails_NoStateChange(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, 1, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(nil, &gitlabapi.Response{}, errors.New("network error"))

	payload := jobPayload("failed", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.False(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_Build_Failed_UnapproveMergeRequestFails_NoStateChange(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, 1, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	client.MockMergeRequestApprovals.EXPECT().
		GetConfiguration(83, int64(7)).
		Return(&gitlabapi.MergeRequestApprovals{UserHasApproved: true}, &gitlabapi.Response{}, nil)

	client.MockMergeRequestApprovals.EXPECT().
		UnapproveMergeRequest(83, int64(7)).
		Return(&gitlabapi.Response{}, errors.New("network error"))

	payload := jobPayload("failed", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.False(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_OnRetryCommand_isRunningJob_ReturnsFalse(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	opts := &gitlabapi.ListJobsOptions{
		ListOptions: gitlabapi.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().ListPipelineJobs(83, int64(10), opts).
		Return([]*gitlabapi.Job{{ID: 1, Name: fixture.Cfg.JobName}}, &gitlabapi.Response{}, nil)

	client.MockJobs.EXPECT().
		RetryJob(83, int64(1)).
		Return(&gitlabapi.Job{ID: 1}, &gitlabapi.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s", fixture.Cfg.RetryCommand), 10)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))

	assert.False(t, db.IsRunningJob(fixture.Database, jobKey))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.True(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_OnRetryCommand_isRunningJob_ReturnsTrue(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, 1, 7)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	opts := &gitlabapi.ListJobsOptions{
		ListOptions: gitlabapi.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().ListPipelineJobs(83, int64(10), opts).
		Return([]*gitlabapi.Job{{ID: 1, Name: fixture.Cfg.JobName}}, &gitlabapi.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s", fixture.Cfg.RetryCommand), 10)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))

	// before requesting a retry we ensure this job is already running
	assert.True(t, db.IsRunningJob(fixture.Database, jobKey))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_OnRetryCommand_InsertFails(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	forceReadOnly(t, fixture.Database)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	opts := &gitlabapi.ListJobsOptions{
		ListOptions: gitlabapi.ListOptions{
			PerPage: 100,
		},
	}

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), opts).
		Return([]*gitlabapi.Job{{ID: 1, Name: fixture.Cfg.JobName}}, &gitlabapi.Response{}, nil)

	client.MockJobs.EXPECT().
		RetryJob(83, int64(1)).
		Return(&gitlabapi.Job{ID: 1}, &gitlabapi.Response{}, nil)

	payload := commentPayload("note", fmt.Sprintf("@%s retry", fixture.Cfg.RetryCommand), 1)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	jobKey := fmt.Sprintf("%d_%d_%s", 83, 1, fixture.Cfg.JobName)
	assert.Equal(t, 0, countRunningJobs(t, fixture.Database, jobKey))
}

func TestProcessWebhook_HandleJobWebhook_UpdateFails(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)

	insertRunningJob(t, fixture.Database, jobKey, 0, 7)
	forceReadOnly(t, fixture.Database)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	payload := jobPayload("success", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)

	retryCount, err := db.GetRetryCount(fixture.Database, jobKey)
	assert.NoError(t, err)
	assert.Equal(t, 0, retryCount)

	assert.True(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_HandleJobWebhook_GetRetryCountFails(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)

	insertRunningJob(t, fixture.Database, jobKey, 0, 7)
	updateRetryCount(t, fixture.Database, jobKey, "not-a-number")

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	payload := jobPayload("success", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
}

func TestProcessWebhook_HandleJobWebhook_GetMergeRequestIid_Fails(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)

	// retry_count is valid so getRetryCount succeeds; merge_request_id is
	// corrupted so the very next call, getMergeRequestIid, fails.
	insertRunningJob(t, fixture.Database, jobKey, 1, 7)
	updateMergeRequestIid(t, fixture.Database, jobKey, "not-a-number")

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	payload := jobPayload("success", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
}

func TestProcessWebhook_OnJobInProgress_GetMergeRequestIid_Fails(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, 0, 7)
	updateMergeRequestIid(t, fixture.Database, jobKey, "not-a-number")

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	jobWebhook := &gitlab.JobWebhook{
		GitlabWebhook: gitlab.GitlabWebhook{ObjectKind: "build", ProjectId: 83},
		Id:            55,
		Status:        "success",
		ProjectId:     83,
		PipelineId:    10,
		Name:          fixture.Cfg.JobName,
	}

	fixture.handler(client).OnJobInProgress(jobWebhook, jobKey, 0)

	retryCount, err := db.GetRetryCount(fixture.Database, jobKey)
	assert.NoError(t, err)
	assert.Equal(t, 0, retryCount)
}

func TestProcessWebhook_OnJobFinished_DeleteJobFails(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, fixture.Cfg.RetryAmount, 7)
	forceReadOnly(t, fixture.Database)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	payload := jobPayload("success", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.True(t, db.IsRunningJob(fixture.Database, jobKey))
}

func TestProcessWebhook_OnJobFailure_DeleteJobFails(t *testing.T) {
	fixture := newWebhookTestFixture(t)
	jobKey := buildJobKey(83, 10, fixture.Cfg.JobName)
	insertRunningJob(t, fixture.Database, jobKey, 1, 7)
	forceReadOnly(t, fixture.Database)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))

	payload := jobPayload("failed", 83, 10, 55, fixture.Cfg.JobName)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := fixture.sendRequest(client, request)

	assert.Equal(t, 200, rec.Code)
	assert.True(t, db.IsRunningJob(fixture.Database, jobKey))

	retryCount, err := db.GetRetryCount(fixture.Database, jobKey)
	assert.NoError(t, err)
	assert.Equal(t, 1, retryCount)
}

func TestProcessWebhook_Unauthorized_Webhook(t *testing.T) {
	fixture := newWebhookTestFixture(t)

	cfgTemplate := `
		server_ip = "127.0.0.1"
		server_port = 8080
		job_name = "job_test"
		retry_command = "@plumber"
		retry_amount = 3
		gitlab_instance = "https://gitlab.example.test"
		access_token = "test-access-token"
		signing_token = "whsec_MzZmYTQ5ZGItZDNhMi00ZjJlLWFkOWYtN2E5YjJjOGQ1ZjFh"
		max_timestamp_skew = "5m"
	`

	fixture.setConfig(t, cfgTemplate)

	client := gitlabtesting.NewTestClient(t, gitlabapi.WithBaseURL(fixture.Cfg.GitlabInstance))
	payload := commentPayload("comment", "@plumber", 10)
	request := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))

	fixture.sendRequest(client, request)

}
