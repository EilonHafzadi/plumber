package gitlab

import (
	"fmt"
	"plumber/internal/config"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
)

func NewGitlabClient(settings *config.Config) (*gitlab.Client, error) {
	client, err := gitlab.NewClient(
		settings.AccessToken,
		gitlab.WithBaseURL(settings.GitlabInstance),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create gitlab client: %w", err)
	}

	return client, nil
}

func RetryJob(gitlabClient *gitlab.Client, projectId int, jobId int64) (*gitlab.Job, error) {
	job, _, err := gitlabClient.Jobs.RetryJob(projectId, jobId)

	if err != nil {
		return nil, err
	}

	return job, nil
}

func GetJobId(gitlabClient *gitlab.Client, webhook *CommentWebhook, cfg *config.Config) (int64, error) {
	opts := &gitlab.ListJobsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
	}

	for {
		jobs, resp, err := gitlabClient.Jobs.ListPipelineJobs(
			webhook.ProjectId,
			int64(webhook.MergeRequest.HeadPipelineId),
			opts,
		)

		if err != nil {
			return -1, err
		}

		if len(jobs) == 0 {
			return -1, fmt.Errorf("no jobs found for pipeline %d", webhook.MergeRequest.HeadPipelineId)
		}

		for _, job := range jobs {
			if job.Name == cfg.JobName {
				return job.ID, nil
			}
		}

		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	return -1, fmt.Errorf("no job with name %s", cfg.JobName)
}

func ApproveMergeRequest(gitlabClient *gitlab.Client, jobWebhook *JobWebhook, mergeRequestIid int64) (bool, error) {
	approvals, _, err := gitlabClient.MergeRequestApprovals.GetConfiguration(jobWebhook.ProjectId, mergeRequestIid)

	if err != nil {
		return false, fmt.Errorf("could not get configuration of merge request %d: %w", mergeRequestIid, err)
	}

	if approvals.UserHasApproved {
		return false, nil
	}

	_, _, err = gitlabClient.MergeRequestApprovals.ApproveMergeRequest(jobWebhook.ProjectId, mergeRequestIid, nil)

	if err != nil {
		return false, fmt.Errorf("could not approve merge request: %d: %w", mergeRequestIid, err)
	}

	return true, nil
}

func UnapproveMergeRequest(gitlabClient *gitlab.Client, jobWebhook *JobWebhook, mergeRequestIid int64) (bool, error) {
	approvals, _, err := gitlabClient.MergeRequestApprovals.GetConfiguration(jobWebhook.ProjectId, mergeRequestIid)

	if err != nil {
		return false, fmt.Errorf("could not get configuration of merge request %d: %w", mergeRequestIid, err)
	}

	if !approvals.UserHasApproved {
		return false, nil
	}

	_, err = gitlabClient.MergeRequestApprovals.UnapproveMergeRequest(jobWebhook.ProjectId, mergeRequestIid)

	if err != nil {
		return false, fmt.Errorf("could not unapprove merge request: %d: %w", mergeRequestIid, err)
	}

	return true, nil
}
