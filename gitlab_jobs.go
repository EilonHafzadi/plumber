package main

import (
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
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
