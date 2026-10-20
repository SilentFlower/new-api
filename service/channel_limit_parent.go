package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ErrChannelLimitParentReadOnly 表示子渠道的继承限制不能直接写入。
var ErrChannelLimitParentReadOnly = errors.New("子渠道限制为只读，请前往父渠道修改")

type channelLimitParentCacheKey struct {
	db        *gorm.DB
	channelID int
}

type channelLimitParentCacheEntry struct {
	value     model.ChannelLimitParent
	expiresAt time.Time
}

var channelLimitParentCache = struct {
	sync.Mutex
	values map[channelLimitParentCacheKey]channelLimitParentCacheEntry
}{values: make(map[channelLimitParentCacheKey]channelLimitParentCacheEntry)}

func cachedChannelLimitParent(ctx context.Context, channelID int) (model.ChannelLimitParent, error) {
	key := channelLimitParentCacheKey{db: model.DB, channelID: channelID}
	now := time.Now()
	channelLimitParentCache.Lock()
	entry, exists := channelLimitParentCache.values[key]
	channelLimitParentCache.Unlock()
	if exists && now.Before(entry.expiresAt) {
		return entry.value, nil
	}
	value, err := model.GetChannelLimitParent(ctx, channelID)
	if err != nil {
		return value, err
	}
	return publishChannelLimitParent(key, value), nil
}

// publishChannelLimitParent 发布单调版本，保存与锁外查询共用同一规则。
func publishChannelLimitParent(key channelLimitParentCacheKey, value model.ChannelLimitParent) model.ChannelLimitParent {
	now := time.Now()
	channelLimitParentCache.Lock()
	defer channelLimitParentCache.Unlock()
	// 查询在锁外完成，迟到的旧快照不能覆盖当前节点已经发布的新版本。
	if current, ok := channelLimitParentCache.values[key]; ok && current.value.Revision > value.Revision {
		return current.value
	}
	if len(channelLimitParentCache.values) >= 4096 {
		for cachedKey, cached := range channelLimitParentCache.values {
			if !now.Before(cached.expiresAt) {
				delete(channelLimitParentCache.values, cachedKey)
			}
		}
		if len(channelLimitParentCache.values) >= 4096 {
			for cachedKey := range channelLimitParentCache.values {
				delete(channelLimitParentCache.values, cachedKey)
				break
			}
		}
	}
	channelLimitParentCache.values[key] = channelLimitParentCacheEntry{value: value, expiresAt: now.Add(5 * time.Second)}
	return value
}

// SaveChannelLimitParent 保存父关系并发布当前节点的新版本，不改配置与计数。
// @param ctx 请求上下文。
// @param value 带旧版本的关系。
// @return 保存错误，成功后 value 含新版本。
func SaveChannelLimitParent(ctx context.Context, value *model.ChannelLimitParent) error {
	if err := model.ReplaceChannelLimitParent(ctx, value); err != nil {
		return err
	}
	key := channelLimitParentCacheKey{db: model.DB, channelID: value.ChannelId}
	publishChannelLimitParent(key, *value)
	return nil
}

// ResolveChannelLimitParent 为新请求解析限制归属，物理渠道及其上游配置保持独立。
// @param ctx 请求上下文。
// @param channel 实际请求渠道。
// @return 顶层限制渠道或存储/非法关系错误，失败时不能当作不限。
func ResolveChannelLimitParent(ctx context.Context, channel *model.Channel) (*model.Channel, error) {
	if channel == nil || channel.Id <= 0 {
		return nil, model.ErrInvalidChannelLimitParent
	}
	relation, err := cachedChannelLimitParent(ctx, channel.Id)
	if err != nil {
		return nil, err
	}
	if relation.ParentChannelId == 0 {
		return channel, nil
	}
	if relation.ParentChannelId <= 0 || relation.ParentChannelId == channel.Id {
		return nil, model.ErrInvalidChannelLimitParent
	}
	parentRelation, err := cachedChannelLimitParent(ctx, relation.ParentChannelId)
	if err != nil {
		return nil, err
	}
	if parentRelation.ParentChannelId != 0 {
		return nil, model.ErrInvalidChannelLimitParent
	}
	parent, err := model.CacheGetChannel(relation.ParentChannelId)
	if err != nil || parent == nil {
		if err == nil {
			err = model.ErrInvalidChannelLimitParent
		}
		return nil, err
	}
	return parent, nil
}

// CheckChannelLimitParentWritable 使用权威关系阻止子渠道的限制写入。
// @param ctx 管理请求上下文。
// @param channelID 当前管理目标。
// @return 子渠道只读或数据库错误。
func CheckChannelLimitParentWritable(ctx context.Context, channelID int) error {
	relation, err := model.GetChannelLimitParent(ctx, channelID)
	if err != nil {
		return err
	}
	if relation.ParentChannelId > 0 {
		return fmt.Errorf("%w (#%d)", ErrChannelLimitParentReadOnly, relation.ParentChannelId)
	}
	return nil
}

// ChannelLimitOwnerID 返回当前尝试冻结的限制归属，兼容没有新快照的旧入口。
// @param c 已选渠上下文。
// @return 限制归属渠道 ID，旧入口回落物理渠道 ID。
func ChannelLimitOwnerID(c *gin.Context) int {
	if ownerID := common.GetContextKeyInt(c, constant.ContextKeyChannelLimitOwnerID); ownerID > 0 {
		return ownerID
	}
	return common.GetContextKeyInt(c, constant.ContextKeyChannelId)
}

// ApplyChannelLimitContext 为选定物理渠道建立限制归属和有效并发上下文。
// @param c 请求上下文。
// @param channel 实际上游渠道。
// @return 解析失败时返回本地错误，不能使用旧尝试的限制快照。
func ApplyChannelLimitContext(c *gin.Context, channel *model.Channel) *types.NewAPIError {
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	owner, err := ResolveChannelLimitParent(ctx, channel)
	if err != nil {
		return ChannelLimitParentAPIError(c, err)
	}
	common.SetContextKey(c, constant.ContextKeyChannelLimitOwnerID, owner.Id)
	ApplyChannelUserEffectiveLimits(c, owner)
	return nil
}

// ChannelLimitParentAPIError 将关系读取失败转换为不可重试且不泄漏存储信息的本地错误。
// @param ctx 用于记录请求关联的诊断日志。
// @param err 关系解析失败。
// @return 503 错误，不触发普通渠道重试。
func ChannelLimitParentAPIError(ctx context.Context, err error) *types.NewAPIError {
	logger.LogWarn(ctx, "解析渠道限制继承失败: "+common.LocalLogPreview(err.Error()))
	return types.NewOpenAIError(errors.New("渠道限制关联暂不可用"), types.ErrorCodeChannelUserConcurrencyUnavailable, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
}
