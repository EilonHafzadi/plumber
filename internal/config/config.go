package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	GitlabInstance      string
	AccessToken         string
	ServerIP            string
	ServerPort          int
	JobName             string
	RetryCommand        string
	RetryAmount         int
	WebhookSigningToken string
	MaxTimestampSkew    string
}

func NewConfig(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")
	v.AddConfigPath(configPath)

	err := v.ReadInConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	return &Config{
		GitlabInstance:      v.GetString("gitlab_instance"),
		AccessToken:         v.GetString("access_token"),
		ServerIP:            v.GetString("server_ip"),
		ServerPort:          v.GetInt("server_port"),
		JobName:             v.GetString("job_name"),
		RetryCommand:        v.GetString("retry_command"),
		RetryAmount:         v.GetInt("retry_amount"),
		WebhookSigningToken: v.GetString("webhook_signing_token"),
		MaxTimestampSkew:    v.GetString("max_timestamp_skew"),
	}, nil
}
