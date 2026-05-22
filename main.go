package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/spf13/viper"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

type Settings struct {
	GitlabInstance string
	AccessToken    string
	ServerIP       string
	ServerPort     int
	JobName        string
	BotName        string
}

var settings Settings

func initSettings(settingsPath string) error {
	viper.SetConfigName("settings")
	viper.SetConfigType("toml")
	viper.AddConfigPath(settingsPath)

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read settings: %w", err)
	}

	settings = Settings{
		GitlabInstance: viper.GetString("gitlab_instance"),
		AccessToken:    viper.GetString("access_token"),
		ServerIP:       viper.GetString("server_ip"),
		ServerPort:     viper.GetInt("server_port"),
		JobName:        viper.GetString("job_name"),
		BotName:        viper.GetString("bot_name"),
	}

	return nil
}

var jobsService gitlab.JobsServiceInterface

func initGitlabClient() error {
	gitlabClient, err := gitlab.NewClient(
		settings.AccessToken,
		gitlab.WithBaseURL(settings.GitlabInstance),
	)

	if err != nil {
		return fmt.Errorf("failed to create gitlab client: %w", err)
	}

	jobsService = gitlabClient.Jobs
	return nil
}

type RetryRequest struct {
	ObjectKind string `json:"object_kind"`
	ProjectId  int    `json:"project_id"`
	JobId      int

	ObjectAttributes struct {
		Note         string `json:"note"`
		NoteableType string `json:"noteable_type"`
	} `json:"object_attributes"`

	MergeRequest struct {
		HeadPipelineId int `json:"head_pipeline_id"`
	} `json:"merge_request"`
}

func getJobId(request *RetryRequest) (int, error) {
	jobs, _, err := jobsService.ListPipelineJobs(
		request.ProjectId,
		int64(request.MergeRequest.HeadPipelineId),
		nil,
	)

	if err != nil {
		return -1, fmt.Errorf("failed to list pipeline jobs: %w", err)
	}

	if len(jobs) == 0 {
		return -1, fmt.Errorf("no jobs found for pipeline %d", request.MergeRequest.HeadPipelineId)
	}

	for _, job := range jobs {
		if job.Name == settings.JobName {
			return int(job.ID), nil
		}
	}

	return -1, fmt.Errorf("failed to find job with name %s", settings.JobName)
}

func retryJob(request *RetryRequest) error {
	_, _, err := jobsService.RetryJob(request.ProjectId, int64(request.JobId))
	if err != nil {
		return fmt.Errorf("failed to retry job: %w", err)
	}

	return nil
}

func onMRComment(responseWriter http.ResponseWriter, retryRequest *RetryRequest) {

	jobId, err := getJobId(retryRequest)
	if err != nil {
		http.Error(responseWriter, "failed to retrieve job id: "+err.Error(), http.StatusInternalServerError)
		return
	}

	retryRequest.JobId = jobId

	if err = retryJob(retryRequest); err != nil {
		http.Error(responseWriter, "failed to retry job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Printf("job %d has been retried\n", jobId)
	responseWriter.WriteHeader(http.StatusOK)
}

func onRetryRequest(responseWriter http.ResponseWriter, httpRequest *http.Request) {
	var retryRequest RetryRequest

	if err := json.NewDecoder(httpRequest.Body).Decode(&retryRequest); err != nil {
		http.Error(responseWriter, "failed to decode request body", http.StatusBadRequest)
		return
	}

	responseWriter.Header().Set("Content-Type", "text/plain")

	if retryRequest.ObjectKind == "note" && retryRequest.ObjectAttributes.NoteableType == "MergeRequest" {
		note := retryRequest.ObjectAttributes.Note

		// if the comment is a bot mention, only then we retry
		if strings.Contains(note, "@"+settings.BotName) {
			onMRComment(responseWriter, &retryRequest)
		}
	}

}

func main() {
	if err := initSettings("."); err != nil {
		log.Fatal(err)
	}

	if err := initGitlabClient(); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/webhook", onRetryRequest)

	serverAddress := fmt.Sprintf("%s:%d", settings.ServerIP, settings.ServerPort)
	fmt.Printf("server has started on address: %s\n", serverAddress)

	if err := http.ListenAndServe(serverAddress, nil); err != nil {
		log.Fatal("failed to start http server: " + err.Error())
	}

}
