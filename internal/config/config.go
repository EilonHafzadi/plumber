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
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(configPath)

	err := viper.ReadInConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	return &Config{
		GitlabInstance:      viper.GetString("gitlab_instance"),
		AccessToken:         viper.GetString("access_token"),
		ServerIP:            viper.GetString("server_ip"),
		ServerPort:          viper.GetInt("server_port"),
		JobName:             viper.GetString("job_name"),
		RetryCommand:        viper.GetString("retry_command"),
		RetryAmount:         viper.GetInt("retry_amount"),
		WebhookSigningToken: viper.GetString("webhook_signing_token"),
		MaxTimestampSkew:    viper.GetString("max_timestamp_skew"),
	}, nil
}
