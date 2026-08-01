package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"gorm.io/gorm"
)

var (
	// ErrTokenLeakScanDisabled 表示扫描在运行前或运行中被管理员关闭。
	ErrTokenLeakScanDisabled = errors.New("token_leak_scan_disabled")
)

const (
	tokenLeakCoverageStatusEnabled   = "enabled"
	tokenLeakCoverageStatusDisabled  = "disabled"
	tokenLeakCoverageStatusExhausted = "exhausted"
	tokenLeakCoverageStatusExpired   = "expired"
	tokenLeakCoverageStatusOther     = "other"
)

// TokenLeakScanCredentialStatus 表示外部扫描与告警凭据的配置状态。
type TokenLeakScanCredentialStatus struct {
	GitHubTokenConfigured     bool `json:"github_token_configured"`
	ScanSecretConfigured      bool `json:"scan_secret_configured"`
	DingTalkWebhookConfigured bool `json:"dingtalk_webhook_configured"`
	DingTalkSigningConfigured bool `json:"dingtalk_signing_configured"`
}

// TokenLeakScanRunSummary 汇总一次全量或单 token 扫描的结果。
type TokenLeakScanRunSummary struct {
	Total              int    `json:"total"`
	Processed          int    `json:"processed"`
	Found              int    `json:"found"`
	NotFound           int    `json:"not_found"`
	Incomplete         int    `json:"incomplete"`
	Failed             int    `json:"failed"`
	SearchRequestCount int    `json:"search_request_count"`
	StoppedReason      string `json:"stopped_reason,omitempty"`
}

// TokenLeakScanCoverageStatus 表示一类令牌的扫描覆盖情况。
type TokenLeakScanCoverageStatus struct {
	Status              string `json:"status"`
	TotalTokens         int    `json:"total_tokens"`
	PendingTokens       int    `json:"pending_tokens"`
	LastScanCompletedAt int64  `json:"last_scan_completed_at"`
}

// TokenLeakScanStatus 汇总管理页面需要的配置、队列与任务状态。
type TokenLeakScanStatus struct {
	Enabled                  bool                          `json:"enabled"`
	IntervalHours            int                           `json:"interval_hours"`
	Credentials              TokenLeakScanCredentialStatus `json:"credentials"`
	GitHubAuthStatus         string                        `json:"github_auth_status"`
	GitHubAuthCheckedAt      int64                         `json:"github_auth_checked_at"`
	TotalTokens              int                           `json:"total_tokens"`
	EnabledTokens            int                           `json:"enabled_tokens"`
	OtherTokens              int                           `json:"other_tokens"`
	ScannedTokens            int                           `json:"scanned_tokens"`
	PendingTokens            int                           `json:"pending_tokens"`
	EstimatedFullScanMinutes int                           `json:"estimated_full_scan_minutes"`
	OpenFindings             int64                         `json:"open_findings"`
	MitigatedFindings        int64                         `json:"mitigated_findings"`
	CoverageByStatus         []TokenLeakScanCoverageStatus `json:"coverage_by_status"`
	CurrentTask              *model.SystemTaskResponse     `json:"current_task"`
	LastTask                 *model.SystemTaskResponse     `json:"last_task"`
	LastScheduledTask        *model.SystemTaskResponse     `json:"last_scheduled_task"`
}

// TokenLeakFindingView 是不包含令牌指纹的管理端泄露位置视图。
type TokenLeakFindingView struct {
	ID               int64                         `json:"id"`
	TokenID          int                           `json:"token_id"`
	UserID           int                           `json:"user_id"`
	TokenName        string                        `json:"token_name"`
	RepositoryID     int64                         `json:"repository_id"`
	RepositoryName   string                        `json:"repository_name"`
	FilePath         string                        `json:"file_path"`
	BlobSHA          string                        `json:"blob_sha"`
	HTMLURL          string                        `json:"html_url"`
	Status           string                        `json:"status"`
	FirstFoundAt     int64                         `json:"first_found_at"`
	LastFoundAt      int64                         `json:"last_found_at"`
	LastNotifiedAt   int64                         `json:"last_notified_at"`
	LastReminderAt   int64                         `json:"last_reminder_at"`
	MitigatedAt      int64                         `json:"mitigated_at"`
	MitigationReason string                        `json:"mitigation_reason"`
	Notifications    []model.TokenLeakNotification `json:"notifications"`
}

// TokenLeakFindingPage 是管理端泄露位置分页响应。
type TokenLeakFindingPage struct {
	Items    []TokenLeakFindingView `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
}

// GetTokenLeakScanCredentialStatus 返回环境变量是否满足扫描与钉钉告警配置。
//
// @return 外部凭据配置状态。
func GetTokenLeakScanCredentialStatus() TokenLeakScanCredentialStatus {
	return TokenLeakScanCredentialStatus{
		GitHubTokenConfigured:     strings.TrimSpace(os.Getenv("GITHUB_TOKEN_LEAK_SCAN_TOKEN")) != "",
		ScanSecretConfigured:      len(os.Getenv("GITHUB_TOKEN_LEAK_SCAN_SECRET")) >= 32,
		DingTalkWebhookConfigured: strings.TrimSpace(os.Getenv("DINGTALK_TOKEN_LEAK_WEBHOOK_TOKEN")) != "",
		DingTalkSigningConfigured: os.Getenv("DINGTALK_TOKEN_LEAK_WEBHOOK_SECRET") != "",
	}
}

// ValidateTokenLeakScanConfiguration 校验启用扫描所需的环境配置。
//
// @return 配置缺失时返回脱敏错误。
func ValidateTokenLeakScanConfiguration() error {
	status := GetTokenLeakScanCredentialStatus()
	if !status.GitHubTokenConfigured {
		return errors.New("github_token_missing")
	}
	if !status.ScanSecretConfigured {
		return errors.New("scan_secret_invalid")
	}
	return nil
}

// GetTokenLeakScanStatus 返回 root 管理页面所需的扫描概览。
//
// @return 扫描概览和数据库错误。
func GetTokenLeakScanStatus() (*TokenLeakScanStatus, error) {
	setting := operation_setting.GetTokenLeakScanSetting()
	now := common.GetTimestamp()
	tokens, err := model.ListTokensForLeakScan()
	if err != nil {
		return nil, err
	}
	states, err := model.ListTokenLeakScanStates()
	if err != nil {
		return nil, err
	}
	stateByTokenID := make(map[int]model.TokenLeakScanState, len(states))
	totalRequests := 0
	for _, state := range states {
		stateByTokenID[state.TokenID] = state
		if state.LastScanCompletedAt > 0 {
			requests := state.SearchRequestCount
			if requests < 1 {
				requests = 1
			}
			totalRequests += requests
		}
	}

	status := &TokenLeakScanStatus{
		Enabled:       setting.Enabled,
		IntervalHours: setting.IntervalHours,
		Credentials:   GetTokenLeakScanCredentialStatus(),
		TotalTokens:   len(tokens),
	}
	coverageOrder := []string{
		tokenLeakCoverageStatusEnabled,
		tokenLeakCoverageStatusDisabled,
		tokenLeakCoverageStatusExhausted,
		tokenLeakCoverageStatusExpired,
		tokenLeakCoverageStatusOther,
	}
	coverageByStatus := make(map[string]*TokenLeakScanCoverageStatus, len(coverageOrder))
	for _, coverageStatus := range coverageOrder {
		coverageByStatus[coverageStatus] = &TokenLeakScanCoverageStatus{Status: coverageStatus}
	}
	for _, token := range tokens {
		if isTokenLeakScanHighPriority(&token, now) {
			status.EnabledTokens++
		} else {
			status.OtherTokens++
		}
		coverage := coverageByStatus[tokenLeakCoverageStatus(&token, now)]
		coverage.TotalTokens++
		state := stateByTokenID[token.Id]
		if state.LastScanCompletedAt > 0 {
			status.ScannedTokens++
			if state.LastScanCompletedAt > coverage.LastScanCompletedAt {
				coverage.LastScanCompletedAt = state.LastScanCompletedAt
			}
		} else {
			coverage.PendingTokens++
		}
	}
	for _, coverageStatus := range coverageOrder {
		status.CoverageByStatus = append(status.CoverageByStatus, *coverageByStatus[coverageStatus])
	}
	status.PendingTokens = status.TotalTokens - status.ScannedTokens
	averageRequests := 1.0
	if status.ScannedTokens > 0 {
		averageRequests = math.Max(1, float64(totalRequests)/float64(status.ScannedTokens))
	}
	status.EstimatedFullScanMinutes = int(math.Ceil(float64(status.TotalTokens) * averageRequests / 8))

	status.OpenFindings, err = model.CountTokenLeakFindingsByStatus(model.TokenLeakFindingStatusOpen)
	if err != nil {
		return nil, err
	}
	status.MitigatedFindings, err = model.CountTokenLeakFindingsByStatus(model.TokenLeakFindingStatusMitigated)
	if err != nil {
		return nil, err
	}
	currentTask, err := getCurrentTokenLeakScanTask()
	if err != nil {
		return nil, err
	}
	if currentTask != nil {
		response := currentTask.ToResponse()
		status.CurrentTask = &response
	}
	lastTasks, err := model.GetLatestSystemTasks([]string{
		model.SystemTaskTypeTokenLeakScan,
		model.SystemTaskTypeTokenLeakScanManual,
	})
	if err != nil {
		return nil, err
	}
	lastScheduledTask := lastTasks[model.SystemTaskTypeTokenLeakScan]
	if lastScheduledTask != nil {
		response := lastScheduledTask.ToResponse()
		status.LastScheduledTask = &response
	}
	lastTask := lastScheduledTask
	lastManualTask := lastTasks[model.SystemTaskTypeTokenLeakScanManual]
	if lastTask == nil || lastManualTask != nil && lastManualTask.ID > lastTask.ID {
		lastTask = lastManualTask
	}
	if lastTask != nil {
		response := lastTask.ToResponse()
		status.LastTask = &response
	}
	latestState, err := model.GetLatestTokenLeakGitHubCheckState()
	if err != nil {
		return nil, err
	}
	status.GitHubAuthStatus = "unknown"
	if latestState != nil {
		status.GitHubAuthCheckedAt = latestState.LastScanCompletedAt
		if latestState.ErrorCode == "auth_failed" {
			status.GitHubAuthStatus = "failed"
		} else if latestState.ScanStatus != model.TokenLeakScanStatusFailed {
			status.GitHubAuthStatus = "ok"
		}
	}
	return status, nil
}

// ListTokenLeakFindingViews 返回不含敏感指纹的泄露位置分页视图。
//
// @param status 可选状态过滤。
// @param page 页码。
// @param pageSize 每页数量。
// @return 分页视图和数据库错误。
func ListTokenLeakFindingViews(status string, page int, pageSize int) (*TokenLeakFindingPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	findings, total, err := model.ListTokenLeakFindings(status, page, pageSize)
	if err != nil {
		return nil, err
	}
	findingIDs := make([]int64, 0, len(findings))
	for _, finding := range findings {
		findingIDs = append(findingIDs, finding.ID)
	}
	notifications, err := model.ListTokenLeakNotificationsByFindingIDs(findingIDs)
	if err != nil {
		return nil, err
	}
	notificationsByFindingID := make(map[int64][]model.TokenLeakNotification, len(findings))
	for _, notification := range notifications {
		notificationsByFindingID[notification.FindingID] = append(notificationsByFindingID[notification.FindingID], notification)
	}
	views := make([]TokenLeakFindingView, 0, len(findings))
	for _, finding := range findings {
		views = append(views, TokenLeakFindingView{
			ID:               finding.ID,
			TokenID:          finding.TokenID,
			UserID:           finding.UserID,
			TokenName:        finding.TokenName,
			RepositoryID:     finding.RepositoryID,
			RepositoryName:   finding.RepositoryName,
			FilePath:         finding.FilePath,
			BlobSHA:          finding.BlobSHA,
			HTMLURL:          finding.HTMLURL,
			Status:           finding.Status,
			FirstFoundAt:     finding.FirstFoundAt,
			LastFoundAt:      finding.LastFoundAt,
			LastNotifiedAt:   finding.LastNotifiedAt,
			LastReminderAt:   finding.LastReminderAt,
			MitigatedAt:      finding.MitigatedAt,
			MitigationReason: finding.MitigationReason,
			Notifications:    notificationsByFindingID[finding.ID],
		})
	}
	return &TokenLeakFindingPage{Items: views, Total: total, Page: page, PageSize: pageSize}, nil
}

// StartTokenLeakScanTask 创建或复用一个手动全量/单 token 扫描任务。
//
// @param tokenID 可选令牌 ID，0 表示手动全量扫描。
// @return 系统任务、是否新建及错误。
func StartTokenLeakScanTask(tokenID int) (*model.SystemTask, bool, error) {
	if !operation_setting.GetTokenLeakScanSetting().Enabled {
		return nil, false, ErrTokenLeakScanDisabled
	}
	if err := ValidateTokenLeakScanConfiguration(); err != nil {
		return nil, false, err
	}
	if tokenID < 0 {
		return nil, false, errors.New("token_id_invalid")
	}
	if tokenID > 0 {
		token, err := model.GetTokenForLeakScanByID(tokenID)
		if err != nil {
			return nil, false, err
		}
		if token == nil {
			return nil, false, errors.New("token_not_found")
		}
	}
	activeTask, err := getCurrentTokenLeakScanTask()
	if err != nil || activeTask != nil {
		return activeTask, false, err
	}
	payload := tokenLeakScanTaskPayload{TokenID: tokenID}
	task, err := model.CreateSystemTaskWithActiveKey(model.SystemTaskTypeTokenLeakScanManual, model.SystemTaskTypeTokenLeakScan, payload, nil)
	if err != nil {
		activeTask, activeErr := getCurrentTokenLeakScanTask()
		if activeErr == nil && activeTask != nil {
			return activeTask, false, nil
		}
		return nil, false, err
	}
	notifySystemTaskRunner()
	return task, true, nil
}

// DisableTokenLeakFindingToken 禁用泄露位置对应的用户令牌并处置其全部开放记录。
//
// @param findingID 泄露位置 ID。
// @return 被禁用的 token ID、所属用户 ID 和错误。
func DisableTokenLeakFindingToken(findingID int64) (int, int, error) {
	if findingID <= 0 {
		return 0, 0, errors.New("finding_id_invalid")
	}
	finding, token, err := model.DisableTokenForLeakFinding(findingID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, 0, errors.New("finding_not_found")
	}
	if err != nil {
		return 0, 0, err
	}
	if token == nil {
		return finding.TokenID, finding.UserID, errors.New("token_not_found")
	}
	return token.Id, token.UserId, nil
}

// HasActiveTokenLeakScanTask 判断当前是否已有全量或手动扫描任务。
//
// @return 是否存在活动任务以及数据库错误。
func HasActiveTokenLeakScanTask() (bool, error) {
	task, err := getCurrentTokenLeakScanTask()
	return task != nil, err
}

// RunTokenLeakScan 执行一次全量或单 token 公开泄露扫描。
//
// @param ctx 任务取消上下文。
// @param tokenID 可选令牌 ID，0 表示全量。
// @param progress 进度回调。
// @return 扫描汇总和执行错误。
func RunTokenLeakScan(ctx context.Context, tokenID int, progress func(processed, total int)) (TokenLeakScanRunSummary, error) {
	if err := ensureTokenLeakScanActive(ctx); err != nil {
		return TokenLeakScanRunSummary{StoppedReason: err.Error()}, err
	}
	if err := ValidateTokenLeakScanConfiguration(); err != nil {
		return TokenLeakScanRunSummary{}, err
	}
	githubToken := strings.TrimSpace(os.Getenv("GITHUB_TOKEN_LEAK_SCAN_TOKEN"))
	scanSecret := []byte(os.Getenv("GITHUB_TOKEN_LEAK_SCAN_SECRET"))
	defer func() {
		for index := range scanSecret {
			scanSecret[index] = 0
		}
	}()
	githubClient, err := newGitHubCodeSearchClient(githubCodeSearchBaseURL, githubToken, newTokenLeakOutboundHTTPClient(30*time.Second), time.Minute/8)
	if err != nil {
		return TokenLeakScanRunSummary{}, err
	}
	githubClient.beforeRequest = ensureTokenLeakScanActive
	notifier := newTokenLeakNotifier(nil)
	notifier.beforeSend = ensureTokenLeakScanActive
	return runTokenLeakScan(ctx, tokenID, scanSecret, githubClient, notifier, progress)
}

func runTokenLeakScan(ctx context.Context, tokenID int, scanSecret []byte, githubClient *githubCodeSearchClient, notifier *tokenLeakNotifier, progress func(processed, total int)) (TokenLeakScanRunSummary, error) {
	if err := ensureTokenLeakScanActive(ctx); err != nil {
		return TokenLeakScanRunSummary{StoppedReason: err.Error()}, err
	}
	if err := reconcileTokenLeakFindings(ctx, notifier); err != nil {
		return TokenLeakScanRunSummary{}, err
	}
	tokens, err := loadTokenLeakScanTokens(tokenID)
	if err != nil {
		return TokenLeakScanRunSummary{}, err
	}
	states, err := model.ListTokenLeakScanStates()
	if err != nil {
		return TokenLeakScanRunSummary{}, err
	}
	stateByTokenID := make(map[int]model.TokenLeakScanState, len(states))
	for _, state := range states {
		stateByTokenID[state.TokenID] = state
	}
	now := common.GetTimestamp()
	sort.SliceStable(tokens, func(left int, right int) bool {
		leftEnabled := isTokenLeakScanHighPriority(&tokens[left], now)
		rightEnabled := isTokenLeakScanHighPriority(&tokens[right], now)
		if leftEnabled != rightEnabled {
			return leftEnabled
		}
		leftScanAt := stateByTokenID[tokens[left].Id].LastScanCompletedAt
		rightScanAt := stateByTokenID[tokens[right].Id].LastScanCompletedAt
		if leftScanAt != rightScanAt {
			return leftScanAt < rightScanAt
		}
		return tokens[left].Id < tokens[right].Id
	})

	summary := TokenLeakScanRunSummary{Total: len(tokens)}
	if progress != nil {
		progress(0, summary.Total)
	}
	for index := range tokens {
		if err := ensureTokenLeakScanActive(ctx); err != nil {
			summary.StoppedReason = err.Error()
			return summary, err
		}
		token := &tokens[index]
		startedAt := common.GetTimestamp()
		fingerprint, anchor, identityErr := deriveTokenLeakIdentity(scanSecret, token.Key)
		if identityErr != nil {
			state := &model.TokenLeakScanState{
				TokenID:             token.Id,
				UserID:              token.UserId,
				TokenStatus:         token.Status,
				ScanStatus:          model.TokenLeakScanStatusFailed,
				ErrorCode:           identityErr.Error(),
				LastScanStartedAt:   startedAt,
				LastScanCompletedAt: common.GetTimestamp(),
			}
			if err := ensureTokenLeakScanActive(ctx); err != nil {
				summary.StoppedReason = err.Error()
				return summary, err
			}
			if err := model.SaveTokenLeakScanState(state); err != nil {
				return summary, err
			}
			summary.Failed++
			summary.Processed++
			token.Key = ""
			if progress != nil {
				progress(summary.Processed, summary.Total)
			}
			continue
		}

		searchResult, searchErr := githubClient.search(ctx, anchor, token.Key)
		summary.SearchRequestCount += searchResult.SearchRequestCount
		if err := ensureTokenLeakScanActive(ctx); err != nil {
			summary.StoppedReason = err.Error()
			return summary, err
		}
		exactMatchCount := 0
		if searchErr == nil {
			exactMatchCount, err = persistTokenLeakCandidates(ctx, token, fingerprint, searchResult.Candidates, notifier)
			if err != nil {
				return summary, err
			}
		}
		scanStatus := model.TokenLeakScanStatusNotFound
		errorCode := ""
		if searchErr != nil {
			scanStatus = model.TokenLeakScanStatusFailed
			errorCode = searchErr.Error()
		} else if searchResult.Incomplete {
			scanStatus = model.TokenLeakScanStatusIncomplete
			errorCode = searchResult.IncompleteReasonCode
		} else if exactMatchCount > 0 {
			scanStatus = model.TokenLeakScanStatusFound
		}
		state := &model.TokenLeakScanState{
			TokenID:             token.Id,
			UserID:              token.UserId,
			TokenFingerprint:    fingerprint,
			TokenStatus:         token.Status,
			ScanStatus:          scanStatus,
			ErrorCode:           errorCode,
			CandidateCount:      searchResult.CandidateCount,
			ExactMatchCount:     exactMatchCount,
			SearchRequestCount:  searchResult.SearchRequestCount,
			LastScanStartedAt:   startedAt,
			LastScanCompletedAt: common.GetTimestamp(),
		}
		if err := ensureTokenLeakScanActive(ctx); err != nil {
			summary.StoppedReason = err.Error()
			return summary, err
		}
		if err := model.SaveTokenLeakScanState(state); err != nil {
			return summary, err
		}
		switch scanStatus {
		case model.TokenLeakScanStatusFound:
			summary.Found++
		case model.TokenLeakScanStatusIncomplete:
			summary.Incomplete++
		case model.TokenLeakScanStatusFailed:
			summary.Failed++
		default:
			summary.NotFound++
		}
		summary.Processed++
		token.Key = ""
		if progress != nil {
			progress(summary.Processed, summary.Total)
		}
		var githubErr *githubCodeSearchError
		if searchErr != nil && errors.As(searchErr, &githubErr) && githubErr.fatal {
			return summary, searchErr
		}
	}
	return summary, nil
}

func loadTokenLeakScanTokens(tokenID int) ([]model.Token, error) {
	if tokenID == 0 {
		return model.ListTokensForLeakScan()
	}
	token, err := model.GetTokenForLeakScanByID(tokenID)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, errors.New("token_not_found")
	}
	return []model.Token{*token}, nil
}

func isTokenLeakScanHighPriority(token *model.Token, now int64) bool {
	if token.Status != common.TokenStatusEnabled {
		return false
	}
	if token.ExpiredTime != -1 && token.ExpiredTime < now {
		return false
	}
	return token.UnlimitedQuota || token.RemainQuota > 0
}

func tokenLeakCoverageStatus(token *model.Token, now int64) string {
	if token.Status == common.TokenStatusEnabled {
		if token.ExpiredTime != -1 && token.ExpiredTime < now {
			return tokenLeakCoverageStatusExpired
		}
		if !token.UnlimitedQuota && token.RemainQuota <= 0 {
			return tokenLeakCoverageStatusExhausted
		}
		return tokenLeakCoverageStatusEnabled
	}
	switch token.Status {
	case common.TokenStatusDisabled:
		return tokenLeakCoverageStatusDisabled
	case common.TokenStatusExhausted:
		return tokenLeakCoverageStatusExhausted
	case common.TokenStatusExpired:
		return tokenLeakCoverageStatusExpired
	default:
		return tokenLeakCoverageStatusOther
	}
}

func ensureTokenLeakScanActive(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !operation_setting.GetTokenLeakScanSetting().Enabled {
		return ErrTokenLeakScanDisabled
	}
	return nil
}

func persistTokenLeakCandidates(ctx context.Context, token *model.Token, fingerprint string, candidates []githubCodeCandidate, notifier *tokenLeakNotifier) (int, error) {
	exactMatchCount := 0
	now := common.GetTimestamp()
	for _, candidate := range candidates {
		if err := ensureTokenLeakScanActive(ctx); err != nil {
			return exactMatchCount, err
		}
		findingKey := tokenLeakFindingKey(token.Id, candidate.RepositoryID, candidate.Path)
		finding, err := model.GetTokenLeakFindingByKey(findingKey)
		if err != nil {
			return exactMatchCount, err
		}
		trigger := ""
		if finding == nil {
			finding = &model.TokenLeakFinding{
				FindingKey:       findingKey,
				TokenID:          token.Id,
				UserID:           token.UserId,
				TokenName:        token.Name,
				TokenFingerprint: fingerprint,
				RepositoryID:     candidate.RepositoryID,
				RepositoryName:   candidate.RepositoryName,
				FilePath:         candidate.Path,
				BlobSHA:          candidate.SHA,
				HTMLURL:          candidate.HTMLURL,
				Status:           model.TokenLeakFindingStatusOpen,
				FirstFoundAt:     now,
				LastFoundAt:      now,
			}
			if token.Status == common.TokenStatusDisabled {
				finding.Status = model.TokenLeakFindingStatusMitigated
				finding.MitigatedAt = now
				finding.MitigationReason = "token_disabled"
			}
			if err := ensureTokenLeakScanActive(ctx); err != nil {
				return exactMatchCount, err
			}
			if err := model.CreateTokenLeakFinding(finding); err != nil {
				return exactMatchCount, err
			}
			trigger = tokenLeakNotifyTriggerFirst
		} else {
			updates := map[string]any{
				"token_fingerprint": fingerprint,
				"repository_name":   candidate.RepositoryName,
				"blob_sha":          candidate.SHA,
				"html_url":          candidate.HTMLURL,
				"last_found_at":     now,
			}
			if finding.TokenName == "" && token.Name != "" {
				updates["token_name"] = token.Name
			}
			if finding.Status == model.TokenLeakFindingStatusMitigated && token.Status == common.TokenStatusEnabled {
				reopenCount := finding.ReopenCount + 1
				updates["status"] = model.TokenLeakFindingStatusOpen
				updates["reopen_count"] = reopenCount
				updates["mitigated_at"] = int64(0)
				updates["mitigation_reason"] = ""
				trigger = tokenLeakReopenedNotificationTrigger(reopenCount)
			}
			if err := ensureTokenLeakScanActive(ctx); err != nil {
				return exactMatchCount, err
			}
			if err := model.UpdateTokenLeakFinding(finding.ID, updates); err != nil {
				return exactMatchCount, err
			}
			finding.TokenFingerprint = fingerprint
			if finding.TokenName == "" {
				finding.TokenName = token.Name
			}
			finding.RepositoryName = candidate.RepositoryName
			finding.BlobSHA = candidate.SHA
			finding.HTMLURL = candidate.HTMLURL
			finding.LastFoundAt = now
			if trigger != "" {
				finding.Status = model.TokenLeakFindingStatusOpen
				finding.ReopenCount++
				finding.MitigatedAt = 0
				finding.MitigationReason = ""
			}
		}
		exactMatchCount++
		if trigger == "" {
			if finding.ReopenCount > 0 {
				trigger = tokenLeakReopenedNotificationTrigger(finding.ReopenCount)
			} else {
				trigger = tokenLeakNotifyTriggerFirst
				notifications, err := model.ListTokenLeakNotificationsByFindingIDs([]int64{finding.ID})
				if err != nil {
					return exactMatchCount, err
				}
				for _, notification := range notifications {
					if notification.Trigger == tokenLeakNotifyTriggerFirst ||
						notification.Trigger == tokenLeakNotifyTriggerReopened ||
						strings.HasPrefix(notification.Trigger, tokenLeakNotifyTriggerReopened+":") {
						trigger = notification.Trigger
						break
					}
				}
			}
		}
		if err := ensureTokenLeakScanActive(ctx); err != nil {
			return exactMatchCount, err
		}
		dingTalkSucceeded, notifyErr := notifier.notifyInitial(ctx, finding, token, trigger)
		if notifyErr != nil {
			return exactMatchCount, notifyErr
		}
		if dingTalkSucceeded {
			finding.LastNotifiedAt = now
			if err := ensureTokenLeakScanActive(ctx); err != nil {
				return exactMatchCount, err
			}
			if err := model.UpdateTokenLeakFinding(finding.ID, map[string]any{"last_notified_at": now}); err != nil {
				return exactMatchCount, err
			}
		}
	}
	return exactMatchCount, nil
}

func reconcileTokenLeakFindings(ctx context.Context, notifier *tokenLeakNotifier) error {
	findings, err := model.ListOpenTokenLeakFindings()
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	for index := range findings {
		if err := ensureTokenLeakScanActive(ctx); err != nil {
			return err
		}
		finding := &findings[index]
		token, err := model.GetTokenForLeakScanByID(finding.TokenID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if token == nil {
			if err := ensureTokenLeakScanActive(ctx); err != nil {
				return err
			}
			if err := model.UpdateTokenLeakFinding(finding.ID, map[string]any{
				"status":            model.TokenLeakFindingStatusMitigated,
				"mitigated_at":      now,
				"mitigation_reason": "token_deleted",
			}); err != nil {
				return err
			}
			continue
		}
		if token.Status == common.TokenStatusDisabled {
			if err := ensureTokenLeakScanActive(ctx); err != nil {
				return err
			}
			if err := model.UpdateTokenLeakFinding(finding.ID, map[string]any{
				"status":            model.TokenLeakFindingStatusMitigated,
				"mitigated_at":      now,
				"mitigation_reason": "token_disabled",
			}); err != nil {
				return err
			}
			continue
		}
		if token.Status != common.TokenStatusEnabled {
			continue
		}
		lastAlertAt := finding.LastNotifiedAt
		if lastAlertAt == 0 {
			lastAlertAt = finding.FirstFoundAt
		}
		if finding.LastReminderAt > lastAlertAt {
			lastAlertAt = finding.LastReminderAt
		}
		if time.Unix(now, 0).Sub(time.Unix(lastAlertAt, 0)) < tokenLeakReminderInterval {
			continue
		}
		attempted, notifyErr := notifier.notifyReminder(ctx, finding)
		if notifyErr != nil {
			return notifyErr
		}
		if attempted {
			if err := ensureTokenLeakScanActive(ctx); err != nil {
				return err
			}
			if err := model.UpdateTokenLeakFinding(finding.ID, map[string]any{"last_reminder_at": now}); err != nil {
				return err
			}
		}
	}
	return nil
}

func tokenLeakFindingKey(tokenID int, repositoryID int64, path string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%d\x00%s", tokenID, repositoryID, path)))
	return hex.EncodeToString(digest[:])
}

func getCurrentTokenLeakScanTask() (*model.SystemTask, error) {
	fullTask, err := model.GetActiveSystemTask(model.SystemTaskTypeTokenLeakScan)
	if err != nil || fullTask != nil {
		return fullTask, err
	}
	return model.GetActiveSystemTask(model.SystemTaskTypeTokenLeakScanManual)
}

type tokenLeakScanTaskPayload struct {
	TokenID int `json:"token_id,omitempty"`
}
