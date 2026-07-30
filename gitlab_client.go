package main

import (
	"fmt"

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

func approveMergeRequest(gitlabClient *gitlab.Client, jobWebhook *JobWebhook, mergeRequestIid int64) (bool, error) {
	approvals, _, err := gitlabClient.MergeRequestApprovals.GetConfiguration(jobWebhook.ProjectId, mergeRequestIid)

	if err != nil {
		logger.Error("failed to get configuration of merge request", zap.Int64("merge_request_id", mergeRequestIid), zap.Error(err))
		return false, err
	}

	if approvals.UserHasApproved {
		return false, nil
	}

	_, _, err = gitlabClient.MergeRequestApprovals.ApproveMergeRequest(jobWebhook.ProjectId, mergeRequestIid, nil)

	if err != nil {
		return false, err
	}

	return true, nil
}

func unapproveMergeRequest(gitlabClient *gitlab.Client, jobWebhook *JobWebhook, mergeRequestIid int64) (bool, error) {
	approvals, _, err := gitlabClient.MergeRequestApprovals.GetConfiguration(jobWebhook.ProjectId, mergeRequestIid)

	if err != nil {
		logger.Error("failed to get configuration of merge request", zap.Int64("merge_request_id", mergeRequestIid), zap.Error(err))
		return false, err
	}

	if !approvals.UserHasApproved {
		return false, nil
	}

	_, err = gitlabClient.MergeRequestApprovals.UnapproveMergeRequest(jobWebhook.ProjectId, mergeRequestIid)

	if err != nil {
		return false, err
	}

	return true, nil
}
