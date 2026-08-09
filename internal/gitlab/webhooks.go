package gitlab

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"plumber/internal/config"
	"plumber/internal/db"
	"strconv"
	"strings"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
	"go.uber.org/zap"
)

type GitlabWebhook struct {
	ObjectKind string `json:"object_kind"`
	ProjectId  int    `json:"project_id"`
}

type CommentWebhook struct {
	GitlabWebhook

	ObjectAttributes struct {
		Note         string `json:"note"`
		NoteableType string `json:"noteable_type"`
	} `json:"object_attributes"`

	MergeRequest struct {
		HeadPipelineId int `json:"head_pipeline_id"`
		Iid            int `json:"iid"`
	} `json:"merge_request"`
}

type JobWebhook struct {
	GitlabWebhook

	Id         int64  `json:"build_id"`
	Status     string `json:"build_status"`
	ProjectId  int    `json:"project_id"`
	PipelineId int    `json:"pipeline_id"`
	Name       string `json:"build_name"`
}

type WebhookHandler struct {
	Client   *gitlab.Client
	Cfg      *config.Config
	Logger   *zap.Logger
	Database *sql.DB
}

func (h *WebhookHandler) onRetryCommand(gitlabClient *gitlab.Client, w http.ResponseWriter, commentWebhook *CommentWebhook) {
	jobId, err := GetJobId(gitlabClient, commentWebhook, h.Cfg)

	if err != nil {
		h.Logger.Error("failed to retrieve job id", zap.Error(err))
		http.Error(w, "failed to retrieve job id: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jobKey := fmt.Sprintf("%d_%d_%s", commentWebhook.ProjectId, commentWebhook.MergeRequest.HeadPipelineId, h.Cfg.JobName)

	if db.IsRunningJob(h.Database, jobKey) {
		h.Logger.Warn("job is already running", zap.String("job_name", h.Cfg.JobName))
		return
	}

	_, err = RetryJob(gitlabClient, commentWebhook.ProjectId, jobId)

	if err != nil {
		h.Logger.Error("failed to retry job", zap.Error(err))
		http.Error(w, "failed to retry job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// otherwise insert it into db
	_, err = h.Database.Exec("INSERT INTO running_jobs (key, retry_count, merge_request_id) VALUES (?, 1, ?)", jobKey, commentWebhook.MergeRequest.Iid)
	if err != nil {
		h.Logger.Error("failed to insert job to running jobs", zap.String("job_name", h.Cfg.JobName), zap.Error(err))
		return
	}

	h.Logger.Info("retrying job of merge request",
		zap.Int("merge_request", commentWebhook.MergeRequest.Iid),
		zap.String("job_name", h.Cfg.JobName),
		zap.Int("retry_count", 1),
	)

	w.WriteHeader(http.StatusCreated)
}

func (h *WebhookHandler) handleCommentWebhook(w http.ResponseWriter, body []byte) {
	var commentWebhook CommentWebhook

	err := json.Unmarshal(body, &commentWebhook)
	if err != nil {
		http.Error(w, "failed to decode comment webhook", http.StatusBadRequest)
		return
	}

	if commentWebhook.ObjectAttributes.NoteableType != "MergeRequest" {
		http.Error(w, "comment is not merge request comment", http.StatusBadRequest)
		return
	}

	note := commentWebhook.ObjectAttributes.Note

	// if the comment is a bot mention, only then we retry
	if strings.Contains(note, h.Cfg.RetryCommand) {
		h.onRetryCommand(h.Client, w, &commentWebhook)
	}

}

func (h *WebhookHandler) onJobInProgress(jobWebhook *JobWebhook, jobKey string, retryCount int) {
	jobName := jobWebhook.Name

	_, err := RetryJob(h.Client, jobWebhook.ProjectId, jobWebhook.Id)
	if err != nil {
		h.Logger.Error("failed to retry job", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	mergeRequestIid, err := db.GetMergeRequestIid(h.Database, jobKey)
	if err != nil {
		h.Logger.Error("failed to get merge request id", zap.Error(err))
		return
	}

	_, err = h.Database.Exec("UPDATE running_jobs SET retry_count = retry_count + 1 WHERE key = ?", jobKey)
	if err != nil {
		h.Logger.Error("failed to update retry_count of job in merge request", zap.Int64("merge_request", mergeRequestIid), zap.String("job_name", jobName), zap.Error(err))
		return
	}

	h.Logger.Info("retrying job of merge request",
		zap.Int64("merge_request", mergeRequestIid),
		zap.String("job_name", h.Cfg.JobName),
		zap.Int("retry_count", retryCount+1),
	)

}

func (h *WebhookHandler) onJobFinished(jobWebhook *JobWebhook, mergeRequestIid int64) {
	jobName := jobWebhook.Name
	jobKey := fmt.Sprintf("%d_%d_%s", jobWebhook.ProjectId, jobWebhook.PipelineId, jobName)

	err := db.DeleteJob(h.Database, jobKey)

	if err != nil {
		h.Logger.Error("failed to delete job from running jobs", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	approved, err := ApproveMergeRequest(h.Client, jobWebhook, mergeRequestIid)
	if err != nil {
		h.Logger.Error("failed to approve merge request", zap.Int64("merge_request", mergeRequestIid), zap.Error(err))
		return
	}

	if approved {
		h.Logger.Info("quality gate passed, merge request approved", zap.Int64("merge_request", mergeRequestIid))
	} else {
		h.Logger.Info("quality gate passed, merge request is already approved", zap.Int64("merge_request", mergeRequestIid))
	}

}

func (h *WebhookHandler) onJobFailure(jobWebhook *JobWebhook, mergeRequestIid int64, retryCount int) {
	jobKey := fmt.Sprintf("%d_%d_%s", jobWebhook.ProjectId, jobWebhook.PipelineId, h.Cfg.JobName)
	err := db.DeleteJob(h.Database, jobKey)

	if err != nil {
		h.Logger.Error("failed to delete job from running jobs", zap.String("job_name", jobWebhook.Name), zap.Error(err))
		return
	}

	unapproved, err := UnapproveMergeRequest(h.Client, jobWebhook, mergeRequestIid)
	jobName := jobWebhook.Name

	if err != nil {
		h.Logger.Error("failed to unapprove merge request", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	if unapproved {
		h.Logger.Error("quality gate failed, merge request unapproved", zap.String("job_name", jobName), zap.Int("retry_count", retryCount))
	} else {
		h.Logger.Error("quality gate failed, job is already unapproved", zap.String("job_name", jobName), zap.Int("retry_count", retryCount))
	}

}

func (h *WebhookHandler) handleJobWebhook(w http.ResponseWriter, body []byte) {
	var jobWebhook JobWebhook

	err := json.Unmarshal(body, &jobWebhook)
	if err != nil {
		http.Error(w, "failed to decode job webhook", http.StatusBadRequest)
		return
	}

	jobName := jobWebhook.Name
	jobKey := fmt.Sprintf("%d_%d_%s", jobWebhook.ProjectId, jobWebhook.PipelineId, jobName)

	// if the job was not triggered by plumber, we don't care about it
	if !db.IsRunningJob(h.Database, jobKey) {
		return
	}

	if jobWebhook.Status != "success" && jobWebhook.Status != "failed" {
		return
	}

	retryCount, err := db.GetRetryCount(h.Database, jobKey)

	if err != nil {
		h.Logger.Error("failed to get retry count of job", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	mergeRequestIid, err := db.GetMergeRequestIid(h.Database, jobKey)
	if err != nil {
		h.Logger.Error("failed to get job merge request id", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	if jobWebhook.Status == "success" && retryCount >= h.Cfg.RetryAmount {
		h.onJobFinished(&jobWebhook, mergeRequestIid)
	} else if jobWebhook.Status == "success" && retryCount < h.Cfg.RetryAmount {
		h.onJobInProgress(&jobWebhook, jobKey, retryCount)
	} else if jobWebhook.Status == "failed" {
		h.onJobFailure(&jobWebhook, mergeRequestIid, retryCount)
	}

}

func (h *WebhookHandler) verifyGitlabWebhook(signingToken string, webhookID string, webhookTimestamp string, signatureHeader string, body []byte) error {
	// if no signing token were defined by user we just skip the verification steps.
	if signingToken == "" {
		return nil
	}

	if webhookID == "" || webhookTimestamp == "" || signatureHeader == "" {
		return errors.New("missing webhook-id, webhook-timestamp, or webhook-signature header")
	}

	tsInt, err := strconv.ParseInt(webhookTimestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid webhook-timestamp: %w", err)
	}

	maxTimestampSkew, err := time.ParseDuration(h.Cfg.MaxTimestampSkew)
	if err != nil {
		return fmt.Errorf("invalid max timestamp skew")
	}

	ts := time.Unix(tsInt, 0)
	if diff := time.Since(ts); diff > maxTimestampSkew || diff < -maxTimestampSkew {
		return fmt.Errorf("webhook timestamp out of tolerance: %v", diff)
	}

	rawKeyB64 := strings.TrimPrefix(signingToken, "whsec_")
	key, err := base64.StdEncoding.DecodeString(rawKeyB64)

	if err != nil {
		return fmt.Errorf("invalid signing token encoding: %w", err)
	}

	message := webhookID + "." + webhookTimestamp + "." + string(body)

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	expected := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))

	for sig := range strings.FieldsSeq(signatureHeader) {
		if subtle.ConstantTimeCompare([]byte(expected), []byte(sig)) == 1 {
			return nil
		}
	}

	return errors.New("signature mismatch")
}

func ProcessWebhook(w http.ResponseWriter, r *http.Request, h *WebhookHandler) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	signingToken := h.Cfg.WebhookSigningToken
	err = h.verifyGitlabWebhook(signingToken, r.Header.Get("webhook-id"), r.Header.Get("webhook-timestamp"), r.Header.Get("webhook-signature"), body)

	if err != nil {
		h.Logger.Error("failed to verify gitlab webhook: ", zap.Error(err))
		return
	}

	var base GitlabWebhook

	err = json.Unmarshal(body, &base)
	if err != nil {
		http.Error(w, "failed to decode webhook", http.StatusBadRequest)
		return
	}

	switch base.ObjectKind {
	case "note":
		h.handleCommentWebhook(w, body)
	case "build":
		h.handleJobWebhook(w, body)
	default:
		http.Error(w, "unsupported webhook type", http.StatusBadRequest)
	}
}
