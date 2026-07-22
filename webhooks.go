package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
)

type GitlabWebhook struct {
	ObjectKind string `json:"object_kind"`
	ProjectId  int    `json:"project_id"`
}

type CommentWebhook struct {
	GitlabWebhook

	JobId int64

	ObjectAttributes struct {
		Note         string `json:"note"`
		NoteableType string `json:"noteable_type"`
	} `json:"object_attributes"`

	MergeRequest struct {
		HeadPipelineId int `json:"head_pipeline_id"`
	} `json:"merge_request"`
}

type JobWebhook struct {
	GitlabWebhook
	Id         int64  `json:"build_id"`
	Status     string `json:"build_status"`
	ProjectId  int    `json:"project_id"`
	PipelineId int    `json:"pipeline_id"`
	Name       string `json:"build_name"`
}

func onMRComment(gitlabClient *gitlab.Client, r http.ResponseWriter, commentWebhook *CommentWebhook) {
	jobId, err := getJobId(gitlabClient, commentWebhook)
	if err != nil {
		http.Error(r, "failed to retrieve job id: "+err.Error(), http.StatusInternalServerError)
		return
	}

	commentWebhook.JobId = jobId

	_, err = retryJob(gitlabClient, commentWebhook.ProjectId, commentWebhook.JobId)

	if err != nil {
		http.Error(r, "failed to retry job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	r.WriteHeader(http.StatusOK)
}

func handleCommentWebhook(gitlabClient *gitlab.Client, w http.ResponseWriter, body []byte) {
	var commentWebhook CommentWebhook

	err := json.Unmarshal(body, &commentWebhook)
	if err != nil {
		http.Error(w, "failed to decode comment webhook", http.StatusBadRequest)
		return
	}

	if commentWebhook.ObjectAttributes.NoteableType != "MergeRequest" {
		return
	}

	note := commentWebhook.ObjectAttributes.Note

	// if the comment is a bot mention, only then we retry
	if strings.Contains(note, "@"+settings.BotName) {
		onMRComment(gitlabClient, w, &commentWebhook)
	}

}

func processWebhook(gitlabClient *gitlab.Client, w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var base GitlabWebhook

	err = json.Unmarshal(body, &base)
	if err != nil {
		http.Error(w, "failed to decode webhook", http.StatusBadRequest)
		return
	}

	switch base.ObjectKind {
	case "note":
		handleCommentWebhook(gitlabClient, w, body)
	case "build":
		handleJobWebhook(gitlabClient, w, body)
	default:
		http.Error(w, "unsupported webhook type", http.StatusBadRequest)
	}
}
