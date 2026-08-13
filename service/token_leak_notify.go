package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const (
	tokenLeakNotifyChannelRoot     = "root"
	tokenLeakNotifyChannelUser     = "user"
	tokenLeakNotifyChannelDingTalk = "dingtalk"
	tokenLeakNotifyTriggerFirst    = "first"
	tokenLeakNotifyTriggerReopened = "reopened"
	tokenLeakNotifyTriggerReminder = "reminder"
	tokenLeakDingTalkMaxAttempts   = 3
	tokenLeakReminderInterval      = 7 * 24 * time.Hour
	dingTalkWebhookBaseURL         = "https://oapi.dingtalk.com/robot/send"
)

type tokenLeakDingTalkPayload struct {
	MsgType  string                    `json:"msgtype"`
	Markdown tokenLeakDingTalkMarkdown `json:"markdown"`
	At       tokenLeakDingTalkAt       `json:"at"`
}

type tokenLeakDingTalkMarkdown struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

type tokenLeakDingTalkAt struct {
	AtMobiles []string `json:"atMobiles"`
	AtUserIDs []string `json:"atUserIds"`
	IsAtAll   bool     `json:"isAtAll"`
}

type tokenLeakDingTalkResponse struct {
	ErrorCode    int    `json:"errcode"`
	ErrorMessage string `json:"errmsg"`
}

type tokenLeakNotifier struct {
	webhookToken  string
	webhookSecret string
	webhookURL    string
	httpClient    *http.Client
	beforeSend    func(context.Context) error
	waitRetry     func(context.Context, time.Duration) error
	getRootUser   func() *model.User
	getUserByID   func(int, bool) (*model.User, error)
	sendUser      func(int, string, dto.UserSetting, dto.Notify) error
}

func newTokenLeakNotifier(httpClient *http.Client) *tokenLeakNotifier {
	if httpClient == nil {
		httpClient = newTokenLeakOutboundHTTPClient(15 * time.Second)
	}
	return &tokenLeakNotifier{
		webhookToken:  strings.TrimSpace(os.Getenv("DINGTALK_TOKEN_LEAK_WEBHOOK_TOKEN")),
		webhookSecret: os.Getenv("DINGTALK_TOKEN_LEAK_WEBHOOK_SECRET"),
		webhookURL:    dingTalkWebhookBaseURL,
		httpClient:    httpClient,
		getRootUser:   model.GetRootUser,
		getUserByID:   model.GetUserById,
		sendUser:      NotifyUser,
		beforeSend: func(ctx context.Context) error {
			return ctx.Err()
		},
		waitRetry: func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func (notifier *tokenLeakNotifier) notifyInitial(ctx context.Context, finding *model.TokenLeakFinding, token *model.Token, trigger string) (bool, error) {
	foundAt := time.Unix(finding.LastFoundAt, 0).UTC().Format(time.RFC3339)
	tokenLabel := fmt.Sprintf("Token ID: %d", finding.TokenID)
	if tokenName := strings.TrimSpace(finding.TokenName); tokenName != "" {
		tokenLabel += "，Token 名称: " + html.EscapeString(tokenLeakAlertSingleLine(tokenName))
	}
	location := html.EscapeString(tokenLeakAlertSingleLine(finding.RepositoryName)) + "/" + html.EscapeString(tokenLeakAlertSingleLine(finding.FilePath))
	rootUser := notifier.getRootUser()
	if rootUser != nil && rootUser.Id > 0 {
		content := fmt.Sprintf(
			"检测到用户 API Key 出现在 GitHub 公开仓库。%s，用户 ID: %d，位置: %s，发现时间: %s。请进入安全告警页面处置：%s",
			tokenLabel,
			finding.UserID,
			location,
			foundAt,
			html.EscapeString(tokenLeakActionURL("/security-alerts/token-leaks")),
		)
		if err := notifier.recordUserNotification(
			ctx,
			finding.ID,
			tokenLeakNotifyChannelRoot,
			trigger,
			rootUser,
			dto.NewNotify(dto.NotifyTypeTokenLeak, "GitHub 公开仓库 Key 泄露告警", content, nil),
		); err != nil {
			return false, err
		}
	}

	owner, err := notifier.getUserByID(token.UserId, false)
	if err != nil {
		return false, err
	}
	if owner != nil && (rootUser == nil || owner.Id != rootUser.Id) {
		content := fmt.Sprintf(
			"检测到您的 API Key 可能出现在 GitHub 公开仓库。%s，用户 ID: %d，位置: %s，发现时间: %s。请立即进入 API Keys 页面禁用或重新创建该 Key：%s",
			tokenLabel,
			finding.UserID,
			location,
			foundAt,
			html.EscapeString(tokenLeakActionURL("/keys")),
		)
		if notifyErr := notifier.recordUserNotification(
			ctx,
			finding.ID,
			tokenLeakNotifyChannelUser,
			trigger,
			owner,
			dto.NewNotify(dto.NotifyTypeTokenLeak, "API Key 公开泄露告警", content, nil),
		); notifyErr != nil {
			return false, notifyErr
		}
	}

	if notifier.webhookToken == "" {
		return false, nil
	}
	dingTalkSucceeded := false
	err = notifier.recordNotification(ctx, finding.ID, tokenLeakNotifyChannelDingTalk, trigger, tokenLeakDingTalkMaxAttempts, func() error {
		if sendErr := notifier.sendDingTalk(ctx, finding, true); sendErr != nil {
			return sendErr
		}
		dingTalkSucceeded = true
		return nil
	})
	return dingTalkSucceeded, err
}

func (notifier *tokenLeakNotifier) notifyReminder(ctx context.Context, finding *model.TokenLeakFinding) (bool, error) {
	if notifier.webhookToken == "" {
		return false, nil
	}
	eventSeed := finding.LastNotifiedAt
	if eventSeed == 0 {
		eventSeed = finding.FirstFoundAt
	}
	if finding.LastReminderAt > eventSeed {
		eventSeed = finding.LastReminderAt
	}
	trigger := tokenLeakNotifyTriggerReminder + ":" + strconv.FormatInt(eventSeed, 10)
	err := notifier.recordNotification(ctx, finding.ID, tokenLeakNotifyChannelDingTalk, trigger, tokenLeakDingTalkMaxAttempts, func() error {
		return notifier.sendDingTalk(ctx, finding, false)
	})
	return true, err
}

func tokenLeakReopenedNotificationTrigger(reopenCount int) string {
	return tokenLeakNotifyTriggerReopened + ":" + strconv.Itoa(reopenCount)
}

func (notifier *tokenLeakNotifier) recordUserNotification(ctx context.Context, findingID int64, channel string, trigger string, user *model.User, data dto.Notify) error {
	setting := user.GetSetting()
	return notifier.recordNotification(ctx, findingID, channel, trigger, 1, func() error {
		if !tokenLeakNotifyDestinationAvailable(user.Email, setting) {
			return errors.New("notification_destination_missing")
		}
		return notifier.sendUser(user.Id, user.Email, setting, data)
	})
}

func (notifier *tokenLeakNotifier) recordNotification(ctx context.Context, findingID int64, channel string, trigger string, maxAttempts int, send func() error) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if err := notifier.beforeSend(ctx); err != nil {
		return err
	}
	eventKey := tokenLeakNotificationEventKey(findingID, channel, trigger)
	existing, err := model.GetTokenLeakNotificationByEventKey(eventKey)
	if err != nil {
		return err
	}
	notification := existing
	if notification != nil {
		if notification.Status == model.TokenLeakNotificationStatusSucceeded || notification.AttemptCount >= maxAttempts {
			return nil
		}
	} else {
		notification = &model.TokenLeakNotification{
			EventKey:  eventKey,
			FindingID: findingID,
			Channel:   channel,
			Trigger:   trigger,
			Status:    model.TokenLeakNotificationStatusPending,
		}
		if err := notifier.beforeSend(ctx); err != nil {
			return err
		}
		if err := model.CreateTokenLeakNotification(notification); err != nil {
			return err
		}
	}
	for attempt := notification.AttemptCount + 1; attempt <= maxAttempts; attempt++ {
		if err := notifier.beforeSend(ctx); err != nil {
			return err
		}
		sendErr := send()
		updates := map[string]any{"attempt_count": attempt}
		if sendErr == nil {
			updates["status"] = model.TokenLeakNotificationStatusSucceeded
			updates["error_code"] = ""
			updates["completed_at"] = common.GetTimestamp()
			return model.UpdateTokenLeakNotification(notification.ID, updates)
		}
		updates["status"] = model.TokenLeakNotificationStatusFailed
		updates["error_code"] = sanitizeTokenLeakNotificationError(sendErr)
		updates["completed_at"] = common.GetTimestamp()
		if err := model.UpdateTokenLeakNotification(notification.ID, updates); err != nil {
			return err
		}
		if attempt < maxAttempts {
			if err := notifier.waitRetry(ctx, time.Duration(attempt)*time.Second); err != nil {
				return err
			}
		}
	}
	return nil
}

func (notifier *tokenLeakNotifier) sendDingTalk(ctx context.Context, finding *model.TokenLeakFinding, atAll bool) error {
	webhookURL, err := url.Parse(notifier.webhookURL)
	if err != nil || webhookURL.Scheme == "" || webhookURL.Host == "" {
		return errors.New("dingtalk_url_invalid")
	}
	query := webhookURL.Query()
	query.Set("access_token", notifier.webhookToken)
	if notifier.webhookSecret != "" {
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		mac := hmac.New(sha256.New, []byte(notifier.webhookSecret))
		_, _ = mac.Write([]byte(timestamp + "\n" + notifier.webhookSecret))
		query.Set("timestamp", timestamp)
		query.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	}
	webhookURL.RawQuery = query.Encode()

	title := "new-api 用户 Key 公开泄露告警"
	tokenName := escapeTokenLeakDingTalkMarkdown(finding.TokenName)
	if tokenName == "" {
		tokenName = "-"
	}
	location := escapeTokenLeakDingTalkMarkdown(finding.RepositoryName) + "/" + escapeTokenLeakDingTalkMarkdown(finding.FilePath)
	publicURL := escapeTokenLeakMarkdownLinkDestination(normalizeGitHubHTMLURL(finding.HTMLURL, finding.RepositoryName))
	actionURL := escapeTokenLeakMarkdownLinkDestination(tokenLeakActionURL("/security-alerts/token-leaks"))
	text := fmt.Sprintf(
		"## %s\n\n- Token ID：%d\n- Token 名称：%s\n- 用户 ID：%d\n- 公开位置：%s\n- 公开页面：[查看 GitHub 文件](%s)\n- 最近发现：%s\n- 处置入口：[打开安全告警](%s)",
		title,
		finding.TokenID,
		tokenName,
		finding.UserID,
		location,
		publicURL,
		time.Unix(finding.LastFoundAt, 0).UTC().Format(time.RFC3339),
		actionURL,
	)
	payload := tokenLeakDingTalkPayload{
		MsgType:  "markdown",
		Markdown: tokenLeakDingTalkMarkdown{Title: title, Text: text},
		At: tokenLeakDingTalkAt{
			AtMobiles: []string{},
			AtUserIDs: []string{},
			IsAtAll:   atAll,
		},
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return errors.New("dingtalk_payload_invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL.String(), strings.NewReader(string(body)))
	if err != nil {
		return errors.New("dingtalk_request_invalid")
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := notifier.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("cancelled")
		}
		return errors.New("dingtalk_network_error")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusTooManyRequests {
			return errors.New("dingtalk_rate_limited")
		}
		return errors.New("dingtalk_http_error")
	}
	responseBody, err := readLimitedBody(response.Body, 64<<10)
	if err != nil {
		return errors.New("dingtalk_response_invalid")
	}
	var result tokenLeakDingTalkResponse
	if err := common.Unmarshal(responseBody, &result); err != nil {
		return errors.New("dingtalk_response_invalid")
	}
	if result.ErrorCode != 0 {
		return errors.New("dingtalk_rejected")
	}
	return nil
}

func tokenLeakNotificationEventKey(findingID int64, channel string, trigger string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", findingID, channel, trigger)))
	return hex.EncodeToString(digest[:])
}

func sanitizeTokenLeakNotificationError(err error) string {
	if err == nil {
		return ""
	}
	code := err.Error()
	if strings.HasPrefix(code, "dingtalk_") || code == "cancelled" || code == "notification_destination_missing" {
		return code
	}
	return "notification_failed"
}

func tokenLeakNotifyDestinationAvailable(userEmail string, setting dto.UserSetting) bool {
	notifyType := setting.NotifyType
	if notifyType == "" {
		notifyType = dto.NotifyTypeEmail
	}
	switch notifyType {
	case dto.NotifyTypeEmail:
		return setting.NotificationEmail != "" || userEmail != ""
	case dto.NotifyTypeWebhook:
		return setting.WebhookUrl != ""
	case dto.NotifyTypeBark:
		return setting.BarkUrl != ""
	case dto.NotifyTypeGotify:
		return setting.GotifyUrl != "" && setting.GotifyToken != ""
	default:
		return false
	}
}

func tokenLeakAlertSingleLine(value string) string {
	return strings.TrimSpace(strings.Map(func(char rune) rune {
		// Git 文件名允许控制字符；告警中统一压成空格，避免伪造额外行或字段。
		if unicode.IsControl(char) {
			return ' '
		}
		return char
	}, value))
}

func escapeTokenLeakDingTalkMarkdown(value string) string {
	value = tokenLeakAlertSingleLine(value)
	replacer := strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"#", "\\#",
		">", "\\>",
	)
	return replacer.Replace(value)
}

func escapeTokenLeakMarkdownLinkDestination(value string) string {
	return strings.NewReplacer("\\", "%5C", "(", "%28", ")", "%29", " ", "%20").Replace(tokenLeakAlertSingleLine(value))
}

func tokenLeakActionURL(path string) string {
	base := strings.TrimRight(system_setting.ServerAddress, "/")
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return path
	}
	return base + path
}

func newTokenLeakOutboundHTTPClient(timeout time.Duration) *http.Client {
	baseClient := GetHttpClient()
	transport := http.DefaultTransport
	if baseClient != nil && baseClient.Transport != nil {
		transport = baseClient.Transport
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("redirect_blocked")
		},
	}
}
