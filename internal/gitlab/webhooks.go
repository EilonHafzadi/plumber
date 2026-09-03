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

type SignedToken struct {
	WebhookId        string
	WebhookTimestamp string
	WebhookSignature string
}

func (h *WebhookHandler) OnRetryCommand(gitlabClient *gitlab.Client, w http.ResponseWriter, commentWebhook *CommentWebhook) {
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

func (h *WebhookHandler) HandleCommentWebhook(w http.ResponseWriter, body []byte) {
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
		h.OnRetryCommand(h.Client, w, &commentWebhook)
	}

}

func (h *WebhookHandler) IncrementRetryCount(jobKey string) (bool, error) {
	res, err := h.Database.Exec(
		"UPDATE running_jobs SET retry_count = retry_count + 1 WHERE key = ? AND retry_count < ?",
		jobKey,
		h.Cfg.RetryAmount,
	)

	if err != nil {
		return false, err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected == 1, nil
}

func (h *WebhookHandler) DecrementRetryCount(jobKey string) (bool, error) {
	res, err := h.Database.Exec("UPDATE running_jobs SET retry_count = retry_count - 1 WHERE key = ?", jobKey)
	if err != nil {
		return false, err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected == 1, nil
}

func (h *WebhookHandler) OnJobInProgress(jobWebhook *JobWebhook, jobKey string, retryCount int) {
	jobName := jobWebhook.Name

	mergeRequestIid, err := db.GetMergeRequestIid(h.Database, jobKey)
	if err != nil {
		h.Logger.Error("failed to get merge request iid", zap.Error(err))
		return
	}

	reserved, err := h.IncrementRetryCount(jobKey)

	if err != nil {
		h.Logger.Error("failed to increment retry count of job in merge request",
			zap.Int64("merge_request", mergeRequestIid),
			zap.String("job_name", jobName),
			zap.Error(err),
		)
		return
	}

	if !reserved {
		h.Logger.Warn("retry limit already reached", zap.String("job_name", jobName))
		return
	}

	_, err = RetryJob(h.Client, jobWebhook.ProjectId, jobWebhook.Id)
	if err != nil {
		rolledBack, rollbackErr := h.DecrementRetryCount(jobKey)
		if rollbackErr != nil {
			h.Logger.Error("failed to roll back retry_count of job in merge request", zap.Int64("merge_request", mergeRequestIid), zap.String("job_name", jobName), zap.Error(rollbackErr))
		} else if !rolledBack {
			h.Logger.Error("failed to roll back retry_count of job in merge request", zap.Int64("merge_request", mergeRequestIid), zap.String("job_name", jobName))
		}

		h.Logger.Error("failed to retry job", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	h.Logger.Info("retrying job of merge request",
		zap.Int64("merge_request", mergeRequestIid),
		zap.String("job_name", h.Cfg.JobName),
		zap.Int("retry_count", retryCount+1),
	)

}

func (h *WebhookHandler) OnJobFinished(jobWebhook *JobWebhook, mergeRequestIid int64) {
	jobName := jobWebhook.Name
	jobKey := fmt.Sprintf("%d_%d_%s", jobWebhook.ProjectId, jobWebhook.PipelineId, jobName)

	approved, err := ApproveMergeRequest(h.Client, jobWebhook, mergeRequestIid)
	if err != nil {
		h.Logger.Error("failed to approve merge request", zap.Int64("merge_request", mergeRequestIid), zap.Error(err))
		return
	}

	err = db.DeleteJob(h.Database, jobKey)

	if err != nil {
		h.Logger.Error("failed to delete job from running jobs", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	if approved {
		h.Logger.Info("quality gate passed, merge request approved", zap.Int64("merge_request", mergeRequestIid))
	} else {
		h.Logger.Info("quality gate passed, merge request is already approved", zap.Int64("merge_request", mergeRequestIid))
	}

}

func (h *WebhookHandler) OnJobFailure(jobWebhook *JobWebhook, mergeRequestIid int64, retryCount int) {
	jobKey := fmt.Sprintf("%d_%d_%s", jobWebhook.ProjectId, jobWebhook.PipelineId, h.Cfg.JobName)

	unapproved, err := UnapproveMergeRequest(h.Client, jobWebhook, mergeRequestIid)
	jobName := jobWebhook.Name

	if err != nil {
		h.Logger.Error("failed to unapprove merge request", zap.String("job_name", jobName), zap.Error(err))
		return
	}

	err = db.DeleteJob(h.Database, jobKey)

	if err != nil {
		h.Logger.Error("failed to delete job from running jobs", zap.String("job_name", jobWebhook.Name), zap.Error(err))
		return
	}

	if unapproved {
		h.Logger.Error("quality gate failed, merge request unapproved", zap.String("job_name", jobName), zap.Int("retry_count", retryCount))
	} else {
		h.Logger.Error("quality gate failed, job is already unapproved", zap.String("job_name", jobName), zap.Int("retry_count", retryCount))
	}

}

func (h *WebhookHandler) HandleJobWebhook(w http.ResponseWriter, body []byte) {
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
		h.OnJobFinished(&jobWebhook, mergeRequestIid)
	} else if jobWebhook.Status == "success" && retryCount < h.Cfg.RetryAmount {
		h.OnJobInProgress(&jobWebhook, jobKey, retryCount)
	} else if jobWebhook.Status == "failed" {
		h.OnJobFailure(&jobWebhook, mergeRequestIid, retryCount)
	}

}

func (h *WebhookHandler) verifyGitlabWebhook(signedToken *SignedToken, body []byte) error {
	// if no signing token were defined by user we just skip the verification steps.
	if h.Cfg.WebhookSigningToken == "" {
		return nil
	}

	if signedToken.WebhookId == "" {
		return errors.New("missing webhook-id")
	}

	if signedToken.WebhookSignature == "" {
		return errors.New("missing webhook-signature-header")
	}

	if signedToken.WebhookTimestamp == "" {
		return errors.New("missing webhook-timestamp")
	}

	tsInt, err := strconv.ParseInt(signedToken.WebhookTimestamp, 10, 64)
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

	rawKeyB64 := strings.TrimPrefix(h.Cfg.WebhookSigningToken, "whsec_")
	key, err := base64.StdEncoding.DecodeString(rawKeyB64)

	if err != nil {
		return fmt.Errorf("invalid signing token encoding: %w", err)
	}

	message := signedToken.WebhookId + "." + signedToken.WebhookTimestamp + "." + string(body)

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	expected := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))

	for sig := range strings.FieldsSeq(signedToken.WebhookSignature) {
		if subtle.ConstantTimeCompare([]byte(expected), []byte(sig)) == 1 {
			return nil
		}
	}

	return errors.New("signature mismatch")
}

func ProcessWebhook(w http.ResponseWriter, r *http.Request, h *WebhookHandler) {
	// Limit the request size to 3MB since we only expect lightweight requests such as comment and job webhooks
	const maxReqSize = 3 * 1024 * 1024
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxReqSize))

	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	signedToken := SignedToken{
		WebhookId:        r.Header.Get("webhook-id"),
		WebhookTimestamp: r.Header.Get("webhook-timestamp"),
		WebhookSignature: r.Header.Get("webhook-signature"),
	}

	err = h.verifyGitlabWebhook(&signedToken, payload)

	if err != nil {
		h.Logger.Error("failed to verify gitlab webhook: ", zap.Error(err))
		http.Error(w, "unauthorized webhook", http.StatusUnauthorized)
		return
	}

	var base GitlabWebhook

	err = json.Unmarshal(payload, &base)
	if err != nil {
		h.Logger.Error("failed to decode webhook: ", zap.Error(err))
		http.Error(w, "failed to decode webhook", http.StatusBadRequest)
		return
	}

	switch base.ObjectKind {
	case "note":
		h.HandleCommentWebhook(w, payload)
	case "build":
		h.HandleJobWebhook(w, payload)
	default:
		http.Error(w, "unsupported webhook type "+base.ObjectKind, http.StatusBadRequest)
	}
}
