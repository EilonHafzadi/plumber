package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/spf13/viper"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Settings struct {
	GitlabInstance string
	AccessToken    string
	ServerIP       string
	ServerPort     int
	JobName        string
	BotName        string
}

var settings *Settings

func initSettings(settingsPath string) error {
	viper.SetConfigName("settings")
	viper.SetConfigType("toml")
	viper.AddConfigPath(settingsPath)

	err := viper.ReadInConfig()
	if err != nil {
		return fmt.Errorf("failed to read settings: %w", err)
	}

	settings = &Settings{
		GitlabInstance: viper.GetString("gitlab_instance"),
		AccessToken:    viper.GetString("access_token"),
		ServerIP:       viper.GetString("server_ip"),
		ServerPort:     viper.GetInt("server_port"),
		JobName:        viper.GetString("job_name"),
		BotName:        viper.GetString("bot_name"),
	}

	return nil
}

var logger *zap.Logger

func setupLogger() error {
	config := zap.Config{
		Level:       zap.NewAtomicLevelAt(zap.InfoLevel),
		Development: true,
		Encoding:    "console",

		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:    "time",
			LevelKey:   "level",
			MessageKey: "msg",
			CallerKey:  "caller",

			EncodeLevel: zapcore.CapitalColorLevelEncoder,
			EncodeTime:  zapcore.TimeEncoderOfLayout("15:04:05"),

			// IMPORTANT: custom caller encoder
			EncodeCaller: func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
				// only keep file:line, drop full path
				enc.AppendString(fmt.Sprintf("%s:%d", path.Base(caller.File), caller.Line))
			},

			ConsoleSeparator: " ", // IMPORTANT: prevents tab alignment spacing
		},

		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	l, err := config.Build(zap.AddCaller())

	if err != nil {
		return fmt.Errorf("failed to setup logger: %w", err)
	}

	logger = l
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
	JobId      int    // initialized later

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

	logger.Info("job retried", zap.Int("job_id", jobId))
	responseWriter.WriteHeader(http.StatusOK)
}

func onRetryRequest(responseWriter http.ResponseWriter, httpRequest *http.Request) {
	var retryRequest RetryRequest

	err := json.NewDecoder(httpRequest.Body).Decode(&retryRequest)
	if err != nil {
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
	err := setupLogger()

	if err != nil {
		log.Fatal(err)
	}

	defer func(logger *zap.Logger) {
		err := logger.Sync()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "failed to sync logger: %v\n", err)
		}
	}(logger)

	settingsPath := os.Getenv("SETTINGS_PATH")
	err = initSettings(settingsPath)

	if err != nil {
		logger.Fatal("failed to initialize settings", zap.Error(err))
	}

	err = initGitlabClient()

	if err != nil {
		logger.Fatal("failed to initialize gitlab client", zap.Error(err))
	}

	http.HandleFunc("/webhook", onRetryRequest)

	serverAddress := fmt.Sprintf("%s:%d", settings.ServerIP, settings.ServerPort)
	logger.Info("server started on", zap.String("address", serverAddress))

	err = http.ListenAndServe(serverAddress, nil)

	if err != nil {
		logger.Fatal("failed to start http server", zap.Error(err))
	}

}
