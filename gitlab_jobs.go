package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
	"go.uber.org/zap"
)

func initGitlabClient() (*gitlab.Client, error) {
	client, err := gitlab.NewClient(
		settings.AccessToken,
		gitlab.WithBaseURL(settings.GitlabInstance),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create gitlab client: %w", err)
	}

	return client, nil
}

func retryJob(gitlabClient *gitlab.Client, projectId int, jobId int64) (*gitlab.Job, error) {
	job, _, err := gitlabClient.Jobs.RetryJob(projectId, jobId)

	if err != nil {
		return nil, err
	}

	return job, nil
}

func getJobId(gitlabClient *gitlab.Client, webhook *CommentWebhook) (int64, error) {
	jobs, _, err := gitlabClient.Jobs.ListPipelineJobs(
		webhook.ProjectId,
		int64(webhook.MergeRequest.HeadPipelineId),
		nil,
	)

	if err != nil {
		return -1, fmt.Errorf("failed to list pipeline jobs: %w", err)
	}

	if len(jobs) == 0 {
		return -1, fmt.Errorf("no jobs found for pipeline %d", webhook.MergeRequest.HeadPipelineId)
	}

	for _, job := range jobs {
		if job.Name == settings.JobName {
			return job.ID, nil
		}
	}

	return -1, fmt.Errorf("failed to find job with name %s", settings.JobName)
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
		logger.Error("job failed ", zap.String("job_name", jobName), zap.String("status", jobWebhook.Status))
	}

	if jobWebhook.Status != "success" {
		return
	}

	logger.Info("job passed successfully", zap.String("job_name", jobName), zap.Int64("job_id", jobWebhook.Id))
}
