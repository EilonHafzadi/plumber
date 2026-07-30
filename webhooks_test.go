package main

import (
	"errors"
	"fmt"
	"io"
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

func beforeEach(t *testing.T) {
	setupLogger(t)
	setupSettings(t)

	database, err := initDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	db = database
}

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

func basePayload(objectKind string, note string, headPipelineId int) string {
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

func TestBotMention_ExactMatch_Triggers(t *testing.T) {
	beforeEach(t)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), nil).
		Return([]*gitlab.Job{{ID: 1, Name: settings.JobName}}, &gitlab.Response{}, nil)

	client.MockJobs.EXPECT().
		RetryJob(83, int64(1)).
		Return(&gitlab.Job{ID: 1}, &gitlab.Response{}, nil)

	payload := basePayload("note", fmt.Sprintf("@%s retry", settings.BotName), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := sendRequest(client, request)
	if rec.Code != http.StatusOK {
		t.Errorf("expected %d but got %d", http.StatusOK, rec.Code)
	}

}

func TestProcessWebhook_InvalidObjectKind_DoesNotRetry(t *testing.T) {
	beforeEach(t)

	client := gitlabtesting.NewTestClient(t, gitlab.WithBaseURL(settings.GitlabInstance))

	payload := basePayload("push", fmt.Sprintf("@%s retry", settings.BotName), 1)
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

	payload := basePayload("note", fmt.Sprintf("@%s retry", settings.BotName), 1)
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

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), nil).
		Return([]*gitlab.Job{}, &gitlab.Response{}, nil)

	payload := basePayload("note", fmt.Sprintf("@%s retry", settings.BotName), 1)
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

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), nil).
		Return([]*gitlab.Job{{ID: 2, Name: "other_job"}}, &gitlab.Response{}, nil)

	payload := basePayload("note", fmt.Sprintf("@%s retry", settings.BotName), 1)
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

	client.MockJobs.EXPECT().
		ListPipelineJobs(83, int64(1), nil).
		Return([]*gitlab.Job{{ID: 1, Name: settings.JobName}}, &gitlab.Response{}, nil)

	client.MockJobs.EXPECT().
		RetryJob(gomock.Any(), gomock.Any()).
		Return(nil, &gitlab.Response{}, errors.New("oh no rip"))

	payload := basePayload("note", fmt.Sprintf("@%s retry", settings.BotName), 1)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))

	rec := sendRequest(client, request)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected %d but got %d", http.StatusInternalServerError, rec.Code)
	}

	assertNextMessage(t, "failed to retry job: oh no rip\n", rec)
}
