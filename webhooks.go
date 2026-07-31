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
		Iid            int `json:"iid"`
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

func onMRComment(gitlabClient *gitlab.Client, w http.ResponseWriter, commentWebhook *CommentWebhook) {
	jobId, err := getJobId(gitlabClient, commentWebhook)
	if err != nil {
		http.Error(w, "failed to retrieve job id: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = retryJob(gitlabClient, commentWebhook.ProjectId, jobId)

	if err != nil {
		http.Error(w, "failed to retry job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jobKey := fmt.Sprintf("%d_%d_%s", commentWebhook.ProjectId, commentWebhook.MergeRequest.HeadPipelineId, settings.JobName)

	canRetry, err := canRetry(jobKey)
	if err != nil {
		logger.Error("failed to check if job can be retried: ", zap.Error(err))
		return
	}

	if !canRetry {
		logger.Warn("job is already running", zap.String("job_name", settings.JobName))
		// job already running
		return
	}

	// otherwise insert it into db
	_, err = db.Exec("INSERT INTO running_jobs (key, retry_count, merge_request_id) VALUES (?, 1, ?)", jobKey, commentWebhook.MergeRequest.Iid)
	if err != nil {
		logger.Error("failed to insert job to running jobs", zap.String("job_name", settings.JobName), zap.Error(err))
		return
	}

	logger.Info("retrying job of merge request",
		zap.Int("merge_request", commentWebhook.MergeRequest.Iid),
		zap.String("job_name", settings.JobName),
		zap.Int("retry_count", 1),
	)

	w.WriteHeader(http.StatusCreated)
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

func onJobInProgress(gitlabClient *gitlab.Client, jobWebhook *JobWebhook, jobKey string, retryCount int) {
	jobName := jobWebhook.Name

	_, err := retryJob(gitlabClient, jobWebhook.ProjectId, jobWebhook.Id)
	if err != nil {
		logger.Error("failed to retry job", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	mergeRequestIid, err := getMergeRequestIid(jobKey)
	if err != nil {
		logger.Error("failed to get merge request id", zap.Error(err))
		return
	}

	_, err = db.Exec("UPDATE running_jobs SET retry_count = retry_count + 1 WHERE key = ?", jobKey)
	if err != nil {
		logger.Error("failed to update retry_count of job in merge request", zap.Int64("merge_request", mergeRequestIid), zap.String("job_name", jobName), zap.Error(err))
		return
	}

	logger.Info("retrying job of merge request",
		zap.Int64("merge_request", mergeRequestIid),
		zap.String("job_name", settings.JobName),
		zap.Int("retry_count", retryCount+1),
	)

}

func onJobFinished(gitlabClient *gitlab.Client, jobWebhook *JobWebhook, mergeRequestIid int64) {
	jobName := jobWebhook.Name
	jobKey := fmt.Sprintf("%d_%d_%s", jobWebhook.ProjectId, jobWebhook.PipelineId, jobName)

	err := deleteJob(jobKey)

	if err != nil {
		logger.Error("failed to delete job from running jobs", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	approved, err := approveMergeRequest(gitlabClient, jobWebhook, mergeRequestIid)
	if err != nil {
		logger.Error("failed to approve merge request", zap.Int64("merge_request", mergeRequestIid), zap.Error(err))
		return
	}

	if approved {
		logger.Info("quality gate passed, merge request approved", zap.Int64("merge_request", mergeRequestIid))
	} else {
		logger.Info("quality gate passed, merge request is already approved", zap.Int64("merge_request", mergeRequestIid))
	}

}

func onJobFailure(gitlabClient *gitlab.Client, jobWebhook *JobWebhook, mergeRequestIid int64, retryCount int) {
	unapproved, err := unapproveMergeRequest(gitlabClient, jobWebhook, mergeRequestIid)
	jobName := jobWebhook.Name

	if err != nil {
		logger.Error("failed to unapprove merge request", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	if unapproved {
		logger.Error("quality gate failed, job has failed", zap.String("job_name", jobName), zap.Int("retry_count", retryCount))
	} else {
		logger.Error("quality gate failed, job is already unapproved", zap.String("job_name", jobName), zap.Int("retry_count", retryCount))
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
	jobKey := fmt.Sprintf("%d_%d_%s", jobWebhook.ProjectId, jobWebhook.PipelineId, jobName)

	// if the job was not triggered by plumber, we don't care about it
	if !isPlumberJob(jobKey) {
		return
	}

	if jobWebhook.Status != "success" && jobWebhook.Status != "failed" {
		return
	}

	retryCount, err := getRetryCount(jobKey)

	if err != nil {
		logger.Error("failed to get retry count of job", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	mergeRequestIid, err := getMergeRequestIid(jobKey)
	if err != nil {
		logger.Error("failed to get job merge request id", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	if jobWebhook.Status == "success" && retryCount >= settings.RetryAmount {
		onJobFinished(gitlabClient, &jobWebhook, mergeRequestIid)
	} else if jobWebhook.Status == "success" && retryCount < settings.RetryAmount {
		onJobInProgress(gitlabClient, &jobWebhook, jobKey, retryCount)
	} else if jobWebhook.Status == "failed" {
		onJobFailure(gitlabClient, &jobWebhook, mergeRequestIid, retryCount)
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
