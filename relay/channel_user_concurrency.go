package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type channelUserConcurrencyGuard struct {
	lease       *service.ChannelUserConcurrencyLease
	baseContext context.Context
	cancel      context.CancelFunc
	channelID   int
	userID      int
	limit       int
}

func acquireChannelUserConcurrency(c *gin.Context) (*channelUserConcurrencyGuard, *types.NewAPIError) {
	limit := common.GetContextKeyInt(c, constant.ContextKeyChannelUserConcurrencyLimit)
	if limit <= 0 {
		return nil, nil
	}
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	baseContext := context.Background()
	if c.Request != nil {
		baseContext = c.Request.Context()
	}
	attemptContext, cancel := context.WithCancel(baseContext)
	if c.Request != nil {
		c.Request = c.Request.WithContext(attemptContext)
	}
	lease, err := service.AcquireChannelUserConcurrency(attemptContext, channelID, userID, limit, func(renewErr error) {
		logger.LogWarn(attemptContext, fmt.Sprintf("渠道单用户并发租约丢失: channel_id=%d user_id=%d limit=%d error=%s", channelID, userID, limit, common.LocalLogPreview(renewErr.Error())))
		cancel()
	})
	if err != nil {
		cancel()
		if c.Request != nil {
			c.Request = c.Request.WithContext(baseContext)
		}
		apiErr := newChannelUserConcurrencyError(err)
		logger.LogWarn(baseContext, fmt.Sprintf("获取渠道单用户并发租约失败: channel_id=%d user_id=%d limit=%d error_code=%s", channelID, userID, limit, apiErr.GetErrorCode()))
		return nil, apiErr
	}
	return &channelUserConcurrencyGuard{
		lease:       lease,
		baseContext: baseContext,
		cancel:      cancel,
		channelID:   channelID,
		userID:      userID,
		limit:       limit,
	}, nil
}

func finishChannelUserConcurrency(c *gin.Context, guard *channelUserConcurrencyGuard, currentErr *types.NewAPIError) *types.NewAPIError {
	if guard == nil {
		return currentErr
	}
	if err := guard.lease.Release(guard.baseContext); err != nil {
		logger.LogWarn(guard.baseContext, fmt.Sprintf("释放渠道单用户并发租约失败: channel_id=%d user_id=%d limit=%d error=%s", guard.channelID, guard.userID, guard.limit, common.LocalLogPreview(err.Error())))
	}
	guard.cancel()
	if c.Request != nil {
		c.Request = c.Request.WithContext(guard.baseContext)
	}
	if guard.lease.IsLost() {
		return newChannelUserConcurrencyError(service.ErrChannelUserConcurrencyUnavailable)
	}
	return currentErr
}

func newChannelUserConcurrencyError(err error) *types.NewAPIError {
	if errors.Is(err, service.ErrChannelUserConcurrencyExceeded) {
		return types.NewOpenAIError(
			errors.New("channel user concurrency limit exceeded"),
			types.ErrorCodeChannelUserConcurrencyExceeded,
			http.StatusTooManyRequests,
			types.ErrOptionWithSkipRetry(),
		)
	}
	return types.NewOpenAIError(
		errors.New("channel user concurrency service unavailable"),
		types.ErrorCodeChannelUserConcurrencyUnavailable,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
}

func channelUserConcurrencyMidjourneyError(apiErr *types.NewAPIError) *dto.MidjourneyResponse {
	if apiErr == nil {
		return nil
	}
	code := 4
	if apiErr.GetErrorCode() == types.ErrorCodeChannelUserConcurrencyExceeded {
		code = 30
	}
	return &dto.MidjourneyResponse{
		Code:        code,
		Description: string(apiErr.GetErrorCode()),
		Result:      apiErr.Error(),
	}
}
