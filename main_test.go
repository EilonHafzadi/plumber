package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

type JobsServiceMock struct {
	gitlab.JobsServiceInterface
	jobs     []*gitlab.Job
	listErr  error
	retryErr error
}

func (m *JobsServiceMock) ListPipelineJobs(_ any, _ int64, _ *gitlab.ListJobsOptions, _ ...gitlab.RequestOptionFunc) ([]*gitlab.Job, *gitlab.Response, error) {
	return m.jobs, nil, m.listErr
}

func (m *JobsServiceMock) RetryJob(_ any, _ int64, _ ...gitlab.RequestOptionFunc) (*gitlab.Job, *gitlab.Response, error) {
	return nil, nil, m.retryErr
}

func setup() {
	viper.Reset()
	settings = Settings{}
	jobsService = nil
}

func setMockJobs(jobs []*gitlab.Job, listErr, retryErr error) {
	jobsService = &JobsServiceMock{
		jobs:     jobs,
		listErr:  listErr,
		retryErr: retryErr,
	}
}

func TestInitSettings_Success(t *testing.T) {
	setup()
	dir := t.TempDir()
	content := `
	gitlab_instance = "https://gitlab.example.com"
	access_token    = "secret"
	server_ip       = "127.0.0.1"
	server_port     = 8080
	job_name        = "my-job"
	bot_name        = "mybot"
	`
	if err := os.WriteFile(dir+"/settings.toml", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := initSettings(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assert.Equal(t, "https://gitlab.example.com", settings.GitlabInstance)
	assert.Equal(t, "secret", settings.AccessToken)
	assert.Equal(t, "127.0.0.1", settings.ServerIP)
	assert.Equal(t, 8080, settings.ServerPort)
	assert.Equal(t, "my-job", settings.JobName)
	assert.Equal(t, "mybot", settings.BotName)
}

func TestInitSettings_MissingFile(t *testing.T) {
	setup()
	err := initSettings(t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read settings")
}

func TestInitGitlabClient_Success(t *testing.T) {
	setup()
	settings.AccessToken = "sometoken"
	settings.GitlabInstance = "https://gitlab.example.com"

	err := initGitlabClient()

	assert.NoError(t, err)
	assert.NotNil(t, jobsService)
}

func TestInitGitlabClient_InvalidBaseURL(t *testing.T) {
	setup()
	settings.AccessToken = "sometoken"
	settings.GitlabInstance = "://not a valid url"

	err := initGitlabClient()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create gitlab client")
}

func TestGetJobId_Success(t *testing.T) {
	setup()
	settings.JobName = "my-job"
	setMockJobs([]*gitlab.Job{{ID: 42, Name: "my-job"}}, nil, nil)

	req := &RetryRequest{ProjectId: 1}
	req.MergeRequest.HeadPipelineId = 10

	id, err := getJobId(req)

	assert.NoError(t, err)
	assert.Equal(t, 42, id)
}

func TestGetJobId_EmptyList(t *testing.T) {
	setup()
	settings.JobName = "my-job"
	setMockJobs([]*gitlab.Job{}, nil, nil)

	req := &RetryRequest{ProjectId: 1}
	req.MergeRequest.HeadPipelineId = 10

	id, err := getJobId(req)

	assert.Equal(t, -1, id)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no jobs found")
}

func TestGetJobId_JobNotFound(t *testing.T) {
	setup()
	settings.JobName = "non-existing-job"
	setMockJobs([]*gitlab.Job{{ID: 42, Name: "amigo"}, {ID: 32, Name: "hola"}}, nil, nil)

	id, err := getJobId(&RetryRequest{ProjectId: 83})

	assert.Equal(t, -1, id)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find job with name non-existing-job")
}

func TestGetJobId_APIError(t *testing.T) {
	setup()
	setMockJobs(nil, fmt.Errorf("gitlab api error"), nil)

	req := &RetryRequest{ProjectId: 1}
	req.MergeRequest.HeadPipelineId = 10

	_, err := getJobId(req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list pipeline jobs")
}

func TestRetryJob_Success(t *testing.T) {
	setup()
	setMockJobs(nil, nil, nil)

	err := retryJob(&RetryRequest{ProjectId: 1, JobId: 42})

	assert.NoError(t, err)
}

func TestRetryJob_APIError(t *testing.T) {
	setup()
	setMockJobs(nil, nil, fmt.Errorf("unauthorized"))

	err := retryJob(&RetryRequest{ProjectId: 1, JobId: 42})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retry job")
}

func TestOnRetryRequest_InvalidBody(t *testing.T) {
	setup()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("not-json"))
	w := httptest.NewRecorder()

	onRetryRequest(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "failed to decode request body")
}

func TestOnRetryRequest_IgnoredEventKind(t *testing.T) {
	setup()
	payload := `{"object_kind":"push","project_id":1}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	w := httptest.NewRecorder()

	onRetryRequest(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestOnRetryRequest_NoteOnNonMR(t *testing.T) {
	setup()
	payload := `{
		"object_kind": "note",
		"project_id": 1,
		"object_attributes": {"noteable_type": "Issue", "note": "@mybot retry"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	w := httptest.NewRecorder()

	onRetryRequest(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestOnRetryRequest_NoBotMention(t *testing.T) {
	setup()
	settings.BotName = "mybot"
	// note does not mention the bot — should be ignored
	payload := `{
		"object_kind": "note",
		"project_id": 10,
		"object_attributes": {"noteable_type": "MergeRequest", "note": "just a regular comment"},
		"merge_request": {"head_pipeline_id": 88}
	}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	w := httptest.NewRecorder()

	onRetryRequest(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestOnRetryRequest_MRComment_Success(t *testing.T) {
	setup()
	settings.BotName = "mybot"
	settings.JobName = "my-job"
	setMockJobs([]*gitlab.Job{{ID: 55, Name: "my-job"}}, nil, nil)

	payload := `{
		"object_kind": "note",
		"project_id": 10,
		"object_attributes": {"noteable_type": "MergeRequest", "note": "@mybot retry"},
		"merge_request": {"head_pipeline_id": 88}
	}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	w := httptest.NewRecorder()

	onRetryRequest(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestOnRetryRequest_MRComment_GetJobFails(t *testing.T) {
	setup()
	settings.BotName = "mybot"
	setMockJobs(nil, fmt.Errorf("api down"), nil)

	payload := `{
		"object_kind": "note",
		"project_id": 10,
		"object_attributes": {"noteable_type": "MergeRequest", "note": "@mybot retry"},
		"merge_request": {"head_pipeline_id": 88}
	}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	w := httptest.NewRecorder()

	onRetryRequest(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to retrieve job id")
}

func TestOnRetryRequest_MRComment_RetryFails(t *testing.T) {
	setup()
	settings.BotName = "mybot"
	settings.JobName = "my-job"
	setMockJobs([]*gitlab.Job{{ID: 55, Name: "my-job"}}, nil, fmt.Errorf("forbidden"))

	payload := `{
		"object_kind": "note",
		"project_id": 10,
		"object_attributes": {"noteable_type": "MergeRequest", "note": "@mybot retry"},
		"merge_request": {"head_pipeline_id": 88}
	}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	w := httptest.NewRecorder()

	onRetryRequest(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to retry job")
}
