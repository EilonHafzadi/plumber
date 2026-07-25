package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

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

var db *sql.DB

func initDatabase() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "./plumber.db")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS running_jobs (key VARCHAR(50) PRIMARY KEY, retry_count INTEGER, merge_request_id INTEGER)")
	if err != nil {
		return nil, err
	}

	return db, nil
}

func main() {
	err := initLogger()

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

	settingsPath := os.Getenv("SETTINGS_PATH")
	if settingsPath == "" {
		logger.Fatal("SETTINGS_PATH environment variable is not set")
	}

	err = initSettings(settingsPath)

	if err != nil {
		logger.Fatal("failed to initialize settings", zap.Error(err))
	}

	gitlabClient, err := initGitlabClient()

	if err != nil {
		logger.Fatal("failed to initialize gitlab client", zap.Error(err))
	}

	db, err = initDatabase()
	if err != nil {
		logger.Fatal("failed to initialize database", zap.Error(err))
	}

	defer db.Close()

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		processWebhook(gitlabClient, w, r)
	})

	serverAddress := fmt.Sprintf("%s:%d", settings.ServerIP, settings.ServerPort)
	logger.Info("server started on", zap.String("address", serverAddress))

	err = http.ListenAndServe(serverAddress, nil)

	if err != nil {
		logger.Fatal("failed to start http server", zap.Error(err))
	}

}
