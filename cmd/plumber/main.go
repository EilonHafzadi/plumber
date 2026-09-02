package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"plumber/internal/config"
	"plumber/internal/db"
	"plumber/internal/gitlab"
	"plumber/internal/logging"

	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

const banner = `
______ _                 _               
| ___ \ |               | |              
| |_/ / |_   _ _ __ ___ | |__   ___ _ __ 
|  __/| | | | | '_ ` + "`" + ` _ \| '_ \ / _ \ '__|
| |   | | |_| | | | | | | |_) |  __/ |   
\_|   |_|\__,_|_| |_| |_|_.__/ \___|_|   
`

func main() {
	logger, err := logging.NewLogger()

	if err != nil {
		log.Fatal(err)
	}

	logger.Info(banner)

	defer func(logger *zap.Logger) {
		err := logger.Sync()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "failed to sync logger: %v\n", err)
		}
	}(logger)

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		logger.Fatal("CONFIG_PATH environment variable is not set")
	}

	cfg, err := config.NewConfig(configPath)

	if err != nil {
		logger.Fatal("failed to initialize config", zap.Error(err))
	}

	gitlabClient, err := gitlab.NewGitlabClient(cfg)

	if err != nil {
		logger.Fatal("failed to initialize gitlab client", zap.Error(err))
	}

	database, err := db.NewDatabase("./plumber.db")
	if err != nil {
		logger.Fatal("failed to initialize database", zap.Error(err))
	}

	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			logger.Error("failed to close database", zap.Error(err))
		}
	}(database)

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		gitlab.ProcessWebhook(w, r, &gitlab.WebhookHandler{
			Client:   gitlabClient,
			Cfg:      cfg,
			Logger:   logger,
			Database: database,
		})
	})

	serverAddress := fmt.Sprintf("%s:%d", cfg.ServerIP, cfg.ServerPort)
	logger.Info("plumber started on", zap.String("address", serverAddress))

	err = http.ListenAndServe(serverAddress, nil)

	if err != nil {
		logger.Fatal("failed to start http plumber", zap.Error(err))
	}

}
