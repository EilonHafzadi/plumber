package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewConfig_SequentialDirectoriesAreIsolated(t *testing.T) {
	firstPath := t.TempDir()
	secondPath := t.TempDir()

	writeConfig := func(path, jobName, gitlabInstance string) {
		t.Helper()
		contents := "job_name = \"" + jobName + "\"\n" +
			"gitlab_instance = \"" + gitlabInstance + "\"\n"
		if err := os.WriteFile(filepath.Join(path, "config.toml"), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	writeConfig(firstPath, "first-job", "https://first.example.test")
	writeConfig(secondPath, "second-job", "https://second.example.test")

	firstCfg, err := NewConfig(firstPath)
	if err != nil {
		t.Fatal(err)
	}

	secondCfg, err := NewConfig(secondPath)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "first-job", firstCfg.JobName)
	assert.Equal(t, "https://first.example.test", firstCfg.GitlabInstance)

	assert.Equal(t, "second-job", secondCfg.JobName)
	assert.Equal(t, "https://second.example.test", secondCfg.GitlabInstance)
}
