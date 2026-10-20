package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelLimitParentBlockedRead struct{}

// setupChannelLimitParentTest 显式初始化父子渠道、方言、缓存与并发存储。
func setupChannelLimitParentTest(t *testing.T, useRedis bool) (*model.Channel, *model.Channel) {
	t.Helper()
	now := time.Date(2031, 9, 15, 12, 0, 0, 0, time.Local)
	parent := setupChannelPeriodTest(t, &now, useRedis)
	oldMain, oldLog, oldMemory := common.MainDatabaseType(), common.LogDatabaseType(), common.MemoryCacheEnabled
	oldConcurrency := channelUserConcurrencyMemory
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = false
	channelUserConcurrencyMemory = newChannelUserConcurrencyMemoryStore()
	t.Cleanup(func() {
		common.SetDatabaseTypes(oldMain, oldLog)
		common.MemoryCacheEnabled = oldMemory
		channelUserConcurrencyMemory = oldConcurrency
	})
	parent.UserConcurrencyLimit = common.GetPointer(4)
	require.NoError(t, model.DB.Model(parent).Update("user_concurrency_limit", 4).Error)
	child := &model.Channel{Id: 81, Name: "子渠道", Key: "child-key", UserConcurrencyLimit: common.GetPointer(1)}
	require.NoError(t, model.DB.Create(child).Error)
	return parent, child
}

func TestChannelLimitParentSharesAtomicUserConcurrency(t *testing.T) {
	for _, mode := range []string{"memory", "redis"} {
		t.Run(mode, func(t *testing.T) {
			parent, child := setupChannelLimitParentTest(t, mode == "redis")
			require.NoError(t, SaveChannelLimitParent(t.Context(), &model.ChannelLimitParent{ChannelId: child.Id, ParentChannelId: parent.Id, UpdatedBy: 1}))
			owner, err := ResolveChannelLimitParent(t.Context(), child)
			require.NoError(t, err)
			assert.Equal(t, parent.Id, owner.Id)
			assert.Equal(t, 4, owner.GetUserConcurrencyLimit())
			var leases []*ChannelUserConcurrencyLease
			defer func() {
				for _, lease := range leases {
					require.NoError(t, lease.Release(context.Background()))
				}
			}()
			for _, channel := range []*model.Channel{parent, parent, child, child} {
				resolved, err := ResolveChannelLimitParent(t.Context(), channel)
				require.NoError(t, err)
				lease, err := AcquireChannelUserConcurrency(t.Context(), resolved.Id, 7, resolved.GetUserConcurrencyLimit(), nil)
				require.NoError(t, err)
				leases = append(leases, lease)
			}
			_, err = AcquireChannelUserConcurrency(t.Context(), owner.Id, 7, 4, nil)
			require.ErrorIs(t, err, ErrChannelUserConcurrencyExceeded)
			other, err := AcquireChannelUserConcurrency(t.Context(), owner.Id, 8, 4, nil)
			require.NoError(t, err)
			leases = append(leases, other)
			usage, _, err := GetChannelUserConcurrencyUsage(t.Context(), parent.Id, 7)
			require.NoError(t, err)
			assert.Equal(t, 4, usage)
			require.NoError(t, leases[0].Release(t.Context()))
			next, err := AcquireChannelUserConcurrency(t.Context(), owner.Id, 7, 4, nil)
			require.NoError(t, err)
			leases = append(leases, next)
		})
	}
}

func TestChannelLimitParentPreservesHistoryAndFreezesSettlementOwner(t *testing.T) {
	for _, mode := range []string{"memory", "redis"} {
		t.Run(mode, func(t *testing.T) {
			parent, child := setupChannelLimitParentTest(t, mode == "redis")
			config := budgetConfig(nil, budgetRow("个人每日", "user", "daily", "", 10))
			_, err := SaveChannelPeriodPolicy(t.Context(), parent.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
			require.NoError(t, err)
			require.NoError(t, RecordChannelUserQuotaUsage(t.Context(), parent.Id, 7, 3))
			require.NoError(t, RecordChannelUserQuotaUsage(t.Context(), child.Id, 7, 2))
			relation := &model.ChannelLimitParent{ChannelId: child.Id, ParentChannelId: parent.Id, UpdatedBy: 1}
			require.NoError(t, SaveChannelLimitParent(t.Context(), relation))
			info := &relaycommon.RelayInfo{UserId: 7, OriginModelName: "test-model", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: child.Id, ChannelLimitOwnerID: parent.Id}}
			RecordRelayChannelUserQuotaUsage(t.Context(), info, 1)
			status, err := GetChannelPeriodStatus(t.Context(), parent, 7)
			require.NoError(t, err)
			require.Len(t, status.Metrics, 1)
			assert.EqualValues(t, 4, status.Metrics[0].Used)
			oldUsed, _, _, err := GetChannelUserDailyQuotaUsage(t.Context(), child.Id, 7)
			require.NoError(t, err)
			assert.EqualValues(t, 2, oldUsed)
			task := model.InitTask(constant.TaskPlatform("test"), info)
			require.NotNil(t, task.PrivateData.ChannelLimitOwnerID)
			assert.Equal(t, parent.Id, *task.PrivateData.ChannelLimitOwnerID)
			relation.ParentChannelId = 0
			require.NoError(t, SaveChannelLimitParent(t.Context(), relation))
			owner, err := ResolveChannelLimitParent(t.Context(), child)
			require.NoError(t, err)
			assert.Equal(t, child.Id, owner.Id)
			// 同一个在途尝试不因解绑而更换记账桶；新请求才使用新的归属。
			RecordRelayChannelUserQuotaUsage(t.Context(), info, 1)
			status, err = GetChannelPeriodStatus(t.Context(), parent, 7)
			require.NoError(t, err)
			assert.EqualValues(t, 5, status.Metrics[0].Used)
			assert.Equal(t, child.Id, info.ChannelId)
		})
	}
}

func TestChannelLimitParentBrokenRelationFailsClosed(t *testing.T) {
	_, child := setupChannelLimitParentTest(t, false)
	require.NoError(t, model.DB.Create(&model.ChannelLimitParent{ChannelId: child.Id, ParentChannelId: 99999, Revision: 1, UpdatedBy: 1}).Error)
	owner, err := ResolveChannelLimitParent(t.Context(), child)
	require.Error(t, err)
	assert.Nil(t, owner)
	apiErr := ChannelLimitParentAPIError(t.Context(), err)
	assert.Equal(t, 503, apiErr.StatusCode)
	assert.False(t, ShouldDisableChannel(apiErr))
}

// TestChannelLimitParentLateReadKeepsPublishedAssociation 验证迟到的旧查询不能把新关联回退成独立渠道。
func TestChannelLimitParentLateReadKeepsPublishedAssociation(t *testing.T) {
	parent, child := setupChannelLimitParentTest(t, false)
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	var snapshot sync.Once
	var workers sync.WaitGroup
	t.Cleanup(func() {
		unblock.Do(func() { close(release) })
		workers.Wait()
	})
	require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register("test:channel_limit_parent_snapshot", func(tx *gorm.DB) {
		if tx.Statement.Context.Value(channelLimitParentBlockedRead{}) == true && tx.Statement.Table == "channel_limit_parents" {
			snapshot.Do(func() {
				close(entered)
				<-release
			})
		}
	}))
	type result struct {
		owner *model.Channel
		err   error
	}
	stale := make(chan result, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		owner, err := ResolveChannelLimitParent(context.WithValue(t.Context(), channelLimitParentBlockedRead{}, true), child)
		stale <- result{owner, err}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("旧请求未取得关系快照")
	}
	require.NoError(t, SaveChannelLimitParent(t.Context(), &model.ChannelLimitParent{ChannelId: child.Id, ParentChannelId: parent.Id, UpdatedBy: 1}))
	unblock.Do(func() { close(release) })
	select {
	case value := <-stale:
		require.NoError(t, value.err)
		require.NotNil(t, value.owner)
		assert.Equal(t, parent.Id, value.owner.Id)
	case <-time.After(2 * time.Second):
		t.Fatal("旧请求未恢复")
	}
	current, err := ResolveChannelLimitParent(t.Context(), child)
	require.NoError(t, err)
	assert.Equal(t, parent.Id, current.Id)
}

func TestChannelLimitParentTaskSettlementUsesPersistedOwnerAndLegacyFallback(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "snapshot"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			parent, child := setupChannelLimitParentTest(t, false)
			require.NoError(t, model.DB.AutoMigrate(&model.Task{}, &model.Token{}, &model.Log{}))
			oldLogDB := model.LOG_DB
			model.LOG_DB = model.DB
			t.Cleanup(func() { model.LOG_DB = oldLogDB })
			seedUser(t, 79077, 10000)
			relation := &model.ChannelLimitParent{ChannelId: child.Id, ParentChannelId: parent.Id, UpdatedBy: 1}
			require.NoError(t, SaveChannelLimitParent(t.Context(), relation))
			task := makeTask(79077, child.Id, 200, 0, BillingSourceWallet, 0)
			if !legacy {
				task.PrivateData.ChannelLimitOwnerID = common.GetPointer(parent.Id)
			}
			require.NoError(t, model.DB.Create(task).Error)
			var stored model.Task
			require.NoError(t, model.DB.First(&stored, task.ID).Error)
			if !legacy {
				relation.ParentChannelId = 0
				require.NoError(t, SaveChannelLimitParent(t.Context(), relation))
			}
			RecalculateTaskQuota(t.Context(), &stored, 350, "父渠道归属验证")
			parentUsed, _, _, err := GetChannelUserDailyQuotaUsage(t.Context(), parent.Id, 79077)
			require.NoError(t, err)
			childUsed, _, _, err := GetChannelUserDailyQuotaUsage(t.Context(), child.Id, 79077)
			require.NoError(t, err)
			if legacy {
				assert.Zero(t, parentUsed)
				assert.EqualValues(t, 150, childUsed)
			} else {
				assert.EqualValues(t, 150, parentUsed)
				assert.Zero(t, childUsed)
			}
			var user model.User
			require.NoError(t, model.DB.First(&user, 79077).Error)
			assert.Equal(t, 9850, user.Quota)
			var log model.Log
			require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).First(&log).Error)
			assert.Equal(t, child.Id, log.ChannelId)
			assert.Equal(t, 150, log.Quota)
		})
	}
}
