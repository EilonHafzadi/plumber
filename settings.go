package main

import (
	"fmt"

	"github.com/spf13/viper"
)

type Settings struct {
	GitlabInstance string
	AccessToken    string
	ServerIP       string
	ServerPort     int
	JobName        string
	BotName        string
	RetryAmount    int
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
		RetryAmount:    viper.GetInt("retry_amount"),
	}

	return nil
}
