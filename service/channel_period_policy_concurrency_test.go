package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// channelPeriodBlockedRead 标记需要停在存储边界的测试请求。
type channelPeriodBlockedRead struct{}

// channelPeriodReadResult 将并发读取结果交回测试主线程断言。
type channelPeriodReadResult struct {
	view dto.ChannelPeriodPolicyView
	err  error
}

// channelPeriodRedisReadBarrier 只暂停带标记的 Redis GET，保持其他请求可正常执行。
type channelPeriodRedisReadBarrier struct {
	entered chan struct{}
	release chan struct{}
}

// BeforeProcess 在指定请求发出 GET 前暂停。
// @param ctx 命令上下文。
// @param cmd Redis 命令。
// @return 原上下文及错误。
func (hook *channelPeriodRedisReadBarrier) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if cmd.Name() == "get" && ctx.Value(channelPeriodBlockedRead{}) == true {
		close(hook.entered)
		<-hook.release
	}
	return ctx, nil
}

// AfterProcess 保留 Redis 命令的执行结果。
// @param ctx 命令上下文。
// @param cmd Redis 命令。
// @return 不添加额外错误。
func (*channelPeriodRedisReadBarrier) AfterProcess(ctx context.Context, cmd redis.Cmder) error {
	return nil
}

// BeforeProcessPipeline 保持批量命令正常执行。
// @param ctx 命令上下文。
// @param cmds Redis 命令列表。
// @return 原上下文及错误。
func (*channelPeriodRedisReadBarrier) BeforeProcessPipeline(ctx context.Context, cmds []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

// AfterProcessPipeline 保留批量命令的执行结果。
// @param ctx 命令上下文。
// @param cmds Redis 命令列表。
// @return 不添加额外错误。
func (*channelPeriodRedisReadBarrier) AfterProcessPipeline(ctx context.Context, cmds []redis.Cmder) error {
	return nil
}

// TestChannelPeriodPolicyRedisReadsIndependent 验证慢渠道不会阻塞另一渠道的缓存读取。
// @param t 测试上下文。
func TestChannelPeriodPolicyRedisReadsIndependent(t *testing.T) {
	now := time.Now()
	channel := setupChannelPeriodTest(t, &now, true)
	input := dto.ChannelPeriodPolicyInput{Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 100}}
	_, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, input, 1)
	require.NoError(t, err)
	_, err = SaveChannelPeriodPolicy(t.Context(), channel.Id+1, input, 1)
	require.NoError(t, err)
	hook := &channelPeriodRedisReadBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	common.RDB.AddHook(hook)
	var workers sync.WaitGroup
	var unblock sync.Once
	slow := make(chan channelPeriodReadResult, 1)
	t.Cleanup(func() {
		unblock.Do(func() { close(hook.release) })
		workers.Wait()
	})
	workers.Add(1)
	go func() {
		defer workers.Done()
		view, readErr := GetChannelPeriodPolicy(context.WithValue(t.Context(), channelPeriodBlockedRead{}, true), channel.Id)
		slow <- channelPeriodReadResult{view, readErr}
	}()
	select {
	case <-hook.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("慢请求未到达 Redis 边界")
	}
	fast := make(chan channelPeriodReadResult, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		view, readErr := GetChannelPeriodPolicy(t.Context(), channel.Id+1)
		fast <- channelPeriodReadResult{view, readErr}
	}()
	select {
	case result := <-fast:
		require.NoError(t, result.err)
		assert.Equal(t, 100, result.view.Config.PoolDailyQuotaLimit)
	case <-time.After(2 * time.Second):
		t.Fatal("另一渠道被尚未释放的 Redis 查询阻塞")
	}
	unblock.Do(func() { close(hook.release) })
	select {
	case result := <-slow:
		require.NoError(t, result.err)
		assert.Equal(t, 100, result.view.Config.PoolDailyQuotaLimit)
	case <-time.After(2 * time.Second):
		t.Fatal("慢请求未恢复")
	}
}

// TestChannelPeriodPolicyLateDatabaseReadKeepsLatestRevision 验证锁外旧查询恢复后仍返回已发布的新策略。
// @param t 测试上下文。
func TestChannelPeriodPolicyLateDatabaseReadKeepsLatestRevision(t *testing.T) {
	for _, mode := range []string{"memory", "redis"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			channel := setupChannelPeriodTest(t, &now, mode == "redis")
			config := dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 100, Rules: []dto.ChannelPeriodRule{}}
			data, err := common.Marshal(config)
			require.NoError(t, err)
			// 直接建立权威记录，首次服务层读取必然经过数据库。
			require.NoError(t, model.ReplaceChannelPeriodPolicy(t.Context(), &model.ChannelPeriodPolicy{ChannelId: channel.Id, Config: string(data), UpdatedBy: 1}))
			entered, release := make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			var workers sync.WaitGroup
			t.Cleanup(func() {
				unblock.Do(func() { close(release) })
				workers.Wait()
			})
			require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register("test:period_policy_snapshot", func(tx *gorm.DB) {
				if tx.Statement.Context.Value(channelPeriodBlockedRead{}) == true {
					close(entered)
					<-release
				}
			}))
			stale := make(chan channelPeriodReadResult, 1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				view, readErr := GetChannelPeriodPolicy(context.WithValue(t.Context(), channelPeriodBlockedRead{}, true), channel.Id)
				stale <- channelPeriodReadResult{view, readErr}
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("旧请求未取得数据库快照")
			}
			saved := make(chan channelPeriodReadResult, 1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				config.PoolDailyQuotaLimit = 50
				view, saveErr := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: 1, Config: config}, 2)
				saved <- channelPeriodReadResult{view, saveErr}
			}()
			select {
			case result := <-saved:
				require.NoError(t, result.err)
				assert.Equal(t, 2, result.view.Revision)
			case <-time.After(2 * time.Second):
				t.Fatal("策略保存被停在数据库边界的旧读取阻塞")
			}
			unblock.Do(func() { close(release) })
			select {
			case result := <-stale:
				require.NoError(t, result.err)
				assert.Equal(t, 2, result.view.Revision)
				assert.Equal(t, 50, result.view.Config.PoolDailyQuotaLimit)
			case <-time.After(2 * time.Second):
				t.Fatal("旧请求未恢复")
			}
			current, err := GetChannelPeriodPolicy(t.Context(), channel.Id)
			require.NoError(t, err)
			assert.Equal(t, 2, current.Revision)
			assert.Equal(t, 50, current.Config.PoolDailyQuotaLimit)
		})
	}
}
