package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
	"go.uber.org/zap"
)

type GitlabWebhook struct {
	ObjectKind string `json:"object_kind"`
	ProjectId  int    `json:"project_id"`
}

type CommentWebhook struct {
	GitlabWebhook

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

func getJobId(gitlabClient *gitlab.Client, webhook *CommentWebhook) (int64, error) {
	jobs, _, err := gitlabClient.Jobs.ListPipelineJobs(
		webhook.ProjectId,
		int64(webhook.MergeRequest.HeadPipelineId),
		nil,
	)

	if err != nil {
		return -1, err
	}

	if len(jobs) == 0 {
		return -1, fmt.Errorf("no jobs found for pipeline %d", webhook.MergeRequest.HeadPipelineId)
	}

	for _, job := range jobs {
		if job.Name == settings.JobName {
			return job.ID, nil
		}
	}

	return -1, fmt.Errorf("no job with name %s", settings.JobName)
}

func onMRComment(gitlabClient *gitlab.Client, r http.ResponseWriter, commentWebhook *CommentWebhook) {
	jobId, err := getJobId(gitlabClient, commentWebhook)
	if err != nil {
		http.Error(r, "failed to retrieve job id: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = retryJob(gitlabClient, commentWebhook.ProjectId, jobId)

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
		http.Error(w, "comment is not merge request comment", http.StatusBadRequest)
		return
	}

	note := commentWebhook.ObjectAttributes.Note

	// if the comment is a bot mention, only then we retry
	if strings.Contains(note, "@"+settings.BotName) {
		onMRComment(gitlabClient, w, &commentWebhook)
	}

}

func handleJobWebhook(gitlabClient *gitlab.Client, w http.ResponseWriter, body []byte) {
	var jobWebhook JobWebhook

	err := json.Unmarshal(body, &jobWebhook)
	if err != nil {
		http.Error(w, "failed to decode job webhook", http.StatusBadRequest)
		return
	}

	jobName := jobWebhook.Name

	if jobWebhook.Status == "failed" {
		logger.Error("job has failed ", zap.String("job_name", jobName))
	}

	if jobWebhook.Status != "success" {
		return
	}

	logger.Info("job passed successfully", zap.String("job_name", jobName), zap.Int64("job_id", jobWebhook.Id))
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
