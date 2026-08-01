package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenLeakDingTalkPayloadUsesSignatureAndAtAll(t *testing.T) {
	var received tokenLeakDingTalkPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "bot-token", r.URL.Query().Get("access_token"))
		timestamp := r.URL.Query().Get("timestamp")
		require.NotEmpty(t, timestamp)
		mac := hmac.New(sha256.New, []byte("bot-secret"))
		_, _ = mac.Write([]byte(timestamp + "\n" + "bot-secret"))
		assert.Equal(t, base64.StdEncoding.EncodeToString(mac.Sum(nil)), r.URL.Query().Get("sign"))
		require.NoError(t, common.DecodeJson(r.Body, &received))
		writeJSONResponse(t, w, map[string]any{"errcode": 0, "errmsg": "ok"})
	}))
	defer server.Close()
	notifier := newTokenLeakNotifier(server.Client())
	notifier.webhookToken = "bot-token"
	notifier.webhookSecret = "bot-secret"
	notifier.webhookURL = server.URL
	finding := &model.TokenLeakFinding{
		TokenID:        31,
		UserID:         41,
		RepositoryName: "public/repo",
		FilePath:       "config/key.txt",
		HTMLURL:        "https://github.com/public/repo/blob/main/config/key.txt",
		LastFoundAt:    time.Now().Unix(),
	}

	err := notifier.sendDingTalk(context.Background(), finding, true)
	require.NoError(t, err)
	assert.Equal(t, "markdown", received.MsgType)
	assert.True(t, received.At.IsAtAll)
	assert.Contains(t, received.Markdown.Text, "Token ID：31")
	assert.Contains(t, received.Markdown.Text, "public/repo/config/key.txt")
}

func TestTokenLeakDingTalkPayloadEscapesUntrustedLocation(t *testing.T) {
	var received tokenLeakDingTalkPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, common.DecodeJson(r.Body, &received))
		writeJSONResponse(t, w, map[string]any{"errcode": 0, "errmsg": "ok"})
	}))
	defer server.Close()
	notifier := newTokenLeakNotifier(server.Client())
	notifier.webhookToken = "bot-token"
	notifier.webhookURL = server.URL
	finding := &model.TokenLeakFinding{
		TokenID:        32,
		UserID:         42,
		TokenName:      "prod_[admin]",
		RepositoryName: "public/repo",
		FilePath:       "config/](https://evil.example)\n<img src=x>",
		HTMLURL:        "https://github.com/public/repo/blob/main/config/key.txt",
		LastFoundAt:    1_700_000_000,
	}

	err := notifier.sendDingTalk(context.Background(), finding, true)
	require.NoError(t, err)
	assert.NotContains(t, received.Markdown.Text, "](https://evil.example)")
	assert.NotContains(t, received.Markdown.Text, "\n<img")
	assert.Contains(t, received.Markdown.Text, `config/\]\(https://evil.example\)`)
	assert.Contains(t, received.Markdown.Text, "[查看 GitHub 文件](https://github.com/public/repo/blob/main/config/key.txt)")
	assert.Contains(t, received.Markdown.Text, "2023-11-14T22:13:20Z")
}

func TestTokenLeakInitialNotificationsAreEscapedAndDoNotContainSecrets(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	root := model.User{Id: 1, Username: "root", Email: "root@example.com", AffCode: "root-aff", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	owner := model.User{Id: 2, Username: "owner", Email: "owner@example.com", AffCode: "owner-aff", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&owner).Error)
	token := &model.Token{Id: 33, UserId: owner.Id, Name: "prod-<admin>", Key: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUV", Status: common.TokenStatusEnabled}
	finding := &model.TokenLeakFinding{
		ID:             44,
		TokenID:        token.Id,
		UserID:         token.UserId,
		TokenName:      token.Name,
		RepositoryName: "public/repo",
		FilePath:       "config/<img src=x>\nkey.txt",
		LastFoundAt:    1_700_000_000,
	}
	notifier := newTokenLeakNotifier(nil)
	notifier.webhookToken = ""
	received := make(map[int]dto.Notify)
	notifier.sendUser = func(userID int, _ string, _ dto.UserSetting, data dto.Notify) error {
		received[userID] = data
		return nil
	}

	_, err := notifier.notifyInitial(context.Background(), finding, token, tokenLeakNotifyTriggerFirst)
	require.NoError(t, err)
	require.Len(t, received, 2)
	for userID, notification := range received {
		assert.NotContains(t, notification.Content, token.Key, "用户 %d 的通知不能包含完整令牌", userID)
		assert.NotContains(t, notification.Content, token.Key[8:24], "用户 %d 的通知不能包含搜索锚点", userID)
		assert.NotContains(t, notification.Content, "<img", "用户 %d 的通知不能包含可执行 HTML", userID)
		assert.Contains(t, notification.Content, "&lt;img src=x&gt; key.txt")
		assert.Contains(t, notification.Content, "2023-11-14T22:13:20Z")
		assert.Contains(t, notification.Content, fmt.Sprintf("Token ID: %d", token.Id))
	}
}

func TestTokenLeakNotificationFailureStopsAfterThreeAttempts(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	notifier := newTokenLeakNotifier(nil)
	notifier.waitRetry = func(context.Context, time.Duration) error { return nil }
	attempts := 0
	err := notifier.recordNotification(context.Background(), 77, tokenLeakNotifyChannelDingTalk, tokenLeakNotifyTriggerFirst, tokenLeakDingTalkMaxAttempts, func() error {
		attempts++
		return errors.New("dingtalk_network_error")
	})
	require.NoError(t, err)
	assert.Equal(t, tokenLeakDingTalkMaxAttempts, attempts)
	eventKey := tokenLeakNotificationEventKey(77, tokenLeakNotifyChannelDingTalk, tokenLeakNotifyTriggerFirst)
	notification, err := model.GetTokenLeakNotificationByEventKey(eventKey)
	require.NoError(t, err)
	require.NotNil(t, notification)
	assert.Equal(t, model.TokenLeakNotificationStatusFailed, notification.Status)
	assert.Equal(t, tokenLeakDingTalkMaxAttempts, notification.AttemptCount)
	assert.Equal(t, "dingtalk_network_error", notification.ErrorCode)
	assert.Greater(t, notification.CompletedAt, int64(0))
}

func TestTokenLeakNotificationStopsBeforeSendWhenScanDisabled(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	notifier := newTokenLeakNotifier(nil)
	notifier.beforeSend = func(context.Context) error { return ErrTokenLeakScanDisabled }
	sent := false

	err := notifier.recordNotification(context.Background(), 78, tokenLeakNotifyChannelDingTalk, tokenLeakNotifyTriggerFirst, 1, func() error {
		sent = true
		return nil
	})
	require.ErrorIs(t, err, ErrTokenLeakScanDisabled)
	assert.False(t, sent)
	eventKey := tokenLeakNotificationEventKey(78, tokenLeakNotifyChannelDingTalk, tokenLeakNotifyTriggerFirst)
	notification, lookupErr := model.GetTokenLeakNotificationByEventKey(eventKey)
	require.NoError(t, lookupErr)
	assert.Nil(t, notification)
}

func TestTokenLeakUserNotificationWithoutDestinationIsRecordedAsFailed(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	notifier := newTokenLeakNotifier(nil)
	user := &model.User{Id: 88, Username: "owner", Status: common.UserStatusEnabled}

	err := notifier.recordUserNotification(
		context.Background(),
		99,
		tokenLeakNotifyChannelUser,
		tokenLeakNotifyTriggerFirst,
		user,
		dto.NewNotify(dto.NotifyTypeTokenLeak, "title", "content", nil),
	)
	require.NoError(t, err)
	eventKey := tokenLeakNotificationEventKey(99, tokenLeakNotifyChannelUser, tokenLeakNotifyTriggerFirst)
	notification, err := model.GetTokenLeakNotificationByEventKey(eventKey)
	require.NoError(t, err)
	require.NotNil(t, notification)
	assert.Equal(t, model.TokenLeakNotificationStatusFailed, notification.Status)
	assert.Equal(t, "notification_destination_missing", notification.ErrorCode)
}

func TestTokenLeakFailedInitialDingTalkGetsLaterReminderAttempt(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	notifier := newTokenLeakNotifier(server.Client())
	notifier.webhookToken = "bot-token"
	notifier.webhookURL = server.URL
	notifier.waitRetry = func(context.Context, time.Duration) error { return nil }
	token := model.Token{Id: 61, UserId: 71, Key: "test-key", Status: common.TokenStatusEnabled}
	require.NoError(t, model.DB.Create(&token).Error)
	finding := model.TokenLeakFinding{
		FindingKey:     "reminder-finding",
		TokenID:        token.Id,
		UserID:         token.UserId,
		RepositoryID:   1,
		RepositoryName: "public/repo",
		FilePath:       "leak.txt",
		HTMLURL:        "https://github.com/public/repo/blob/main/leak.txt",
		Status:         model.TokenLeakFindingStatusOpen,
		FirstFoundAt:   time.Now().Add(-8 * 24 * time.Hour).Unix(),
		LastFoundAt:    time.Now().Unix(),
	}
	require.NoError(t, model.CreateTokenLeakFinding(&finding))

	require.NoError(t, reconcileTokenLeakFindings(context.Background(), notifier))
	assert.Equal(t, tokenLeakDingTalkMaxAttempts, attempts)
	storedFinding, err := model.GetTokenLeakFindingByID(finding.ID)
	require.NoError(t, err)
	require.NotNil(t, storedFinding)
	assert.Greater(t, storedFinding.LastReminderAt, int64(0))
	notifications, err := model.ListTokenLeakNotificationsByFindingIDs([]int64{finding.ID})
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, model.TokenLeakNotificationStatusFailed, notifications[0].Status)
	assert.Contains(t, notifications[0].Trigger, tokenLeakNotifyTriggerReminder)

	require.NoError(t, reconcileTokenLeakFindings(context.Background(), notifier))
	assert.Equal(t, tokenLeakDingTalkMaxAttempts, attempts)
}
