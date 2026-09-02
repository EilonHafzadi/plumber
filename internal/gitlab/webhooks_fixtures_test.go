package gitlab_test

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"plumber/internal/config"
	"plumber/internal/db"
	"plumber/internal/gitlab"
	"plumber/internal/logging"
	"testing"

	"github.com/stretchr/testify/assert"
	gitlabtesting "gitlab.com/gitlab-org/api/client-go/v2/testing"
	"go.uber.org/zap"

	_ "github.com/mattn/go-sqlite3"
)

type WebhookTestFixture struct {
	Cfg      *config.Config
	Logger   *zap.Logger
	Database *sql.DB
}

func newWebhookTestFixture(t *testing.T) *WebhookTestFixture {
	t.Helper()

	logger, err := logging.NewLogger()
	if err != nil {
		t.Fatal("failed to init logger: " + err.Error())
	}

	database, err := db.NewDatabase(":memory:")
	if err != nil {
		t.Fatal("failed to init database: " + err.Error())
	}

	t.Cleanup(func() {
		err := database.Close()
		if err != nil {
			t.Fatal(err)
		}
	})

	configTemplate := `
		server_ip = "127.0.0.1"
		server_port = 8080
		job_name = "job_test"
		retry_command = "@plumber"
		retry_amount = 3
		gitlab_instance = "https://gitlab.example.test"
		access_token = "test-access-token"
	`
	cfg := loadConfig(t, configTemplate)

	return &WebhookTestFixture{Cfg: cfg, Logger: logger, Database: database}
}

func loadConfig(t *testing.T, configTemplate string) *config.Config {
	t.Helper()

	configPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(configPath, "config.toml"), []byte(configTemplate), 0o600); err != nil {
		t.Fatal("failed to write test config: " + err.Error())
	}

	cfg, err := config.NewConfig(configPath)
	if err != nil {
		t.Fatal("failed to init config: " + err.Error())
	}

	return cfg
}

func (f *WebhookTestFixture) setConfig(t *testing.T, configTemplate string) {
	f.Cfg = loadConfig(t, configTemplate)
}

func (f *WebhookTestFixture) handler(client *gitlabtesting.TestClient) *gitlab.WebhookHandler {
	return &gitlab.WebhookHandler{
		Client:   client.Client,
		Cfg:      f.Cfg,
		Logger:   f.Logger,
		Database: f.Database,
	}
}

func (f *WebhookTestFixture) sendRequest(client *gitlabtesting.TestClient, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	gitlab.ProcessWebhook(recorder, request, f.handler(client))
	return recorder
}

func commentPayload(objectKind, note string, headPipelineID int) string {
	return fmt.Sprintf(`{
		"object_kind": %q,
		"project_id": 83,
		"object_attributes": {
			"note": %q,
			"noteable_type": "MergeRequest"
		},
		"merge_request": {
			"head_pipeline_id": %d
		}
	}`, objectKind, note, headPipelineID)
}

func jobPayload(status string, projectID, pipelineID int, buildID int64, jobName string) string {
	return fmt.Sprintf(`{
		"object_kind": "build",
		"project_id": %d,
		"build_id": %d,
		"build_status": %q,
		"pipeline_id": %d,
		"build_name": %q
	}`, projectID, buildID, status, pipelineID, jobName)
}

func countRunningJobs(t *testing.T, database *sql.DB, key string) int {
	t.Helper()

	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM running_jobs WHERE key = ?", key).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func buildJobKey(projectID, pipelineID int, jobName string) string {
	return fmt.Sprintf("%d_%d_%s", projectID, pipelineID, jobName)
}

func assertNextMessage(t *testing.T, expected string, recorder *httptest.ResponseRecorder) {
	t.Helper()
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, expected, string(body))
}

func forceReadOnly(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.Exec("PRAGMA query_only = ON"); err != nil {
		t.Fatal(err)
	}
}

func updateRetryCount(t *testing.T, database *sql.DB, key, value string) {
	t.Helper()
	if _, err := database.Exec("UPDATE running_jobs SET retry_count = ? WHERE key = ?", value, key); err != nil {
		t.Fatal(err)
	}
}

func updateMergeRequestIid(t *testing.T, database *sql.DB, key, value string) {
	t.Helper()
	if _, err := database.Exec("UPDATE running_jobs SET merge_request_id = ? WHERE key = ?", value, key); err != nil {
		t.Fatal(err)
	}
}

func insertRunningJob(t *testing.T, database *sql.DB, key string, retryCount int, mergeRequestIID int64) {
	t.Helper()
	if _, err := database.Exec(
		"INSERT INTO running_jobs (key, retry_count, merge_request_id) VALUES (?, ?, ?)",
		key, retryCount, mergeRequestIID,
	); err != nil {
		t.Fatal(err)
	}
}
