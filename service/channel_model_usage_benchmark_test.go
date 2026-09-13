package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	gormlogger "gorm.io/gorm/logger"
)

// setupChannelModelUsageBenchmark 只连接显式提供的隔离 Redis；清空其测试 DB，禁止传入业务实例。
func setupChannelModelUsageBenchmark(b *testing.B, now *time.Time, useRedis bool) *model.Channel {
	b.Helper()
	channel := setupChannelPeriodTest(b, now, false)
	model.DB.Logger = model.DB.Logger.LogMode(gormlogger.Silent)
	if useRedis {
		addr := os.Getenv("CHANNEL_MODEL_USAGE_BENCH_REDIS_ADDR")
		if addr == "" {
			b.Skip("未配置专用模型累计基准 Redis")
		}
		client := redis.NewClient(&redis.Options{Addr: addr})
		require.NoError(b, client.FlushDB(context.Background()).Err())
		common.RedisEnabled, common.RDB = true, client
		b.Cleanup(func() { require.NoError(b, client.Close()) })
	}
	return channel
}

func BenchmarkChannelModelUsageRecord(b *testing.B) {
	for _, useRedis := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("redis=%t/enabled=%t", useRedis, enabled), func(b *testing.B) {
				now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
				channel := setupChannelModelUsageBenchmark(b, &now, useRedis)
				config := budgetConfig(nil)
				config.ModelUsageTrackingEnabled = &enabled
				_, err := SaveChannelPeriodPolicy(context.Background(), channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
				require.NoError(b, err)
				require.NoError(b, RecordChannelUserModelQuotaUsage(context.Background(), channel.Id, 7, 1, "A"))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := RecordChannelUserModelQuotaUsage(context.Background(), channel.Id, 7, 1, "A"); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkChannelModelUsageRead(b *testing.B) {
	for _, useRedis := range []bool{false, true} {
		for _, count := range []int{1, 8, 64} {
			b.Run(fmt.Sprintf("redis=%t/models=%d", useRedis, count), func(b *testing.B) {
				now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
				channel := setupChannelModelUsageBenchmark(b, &now, useRedis)
				models := make([]string, count)
				for i := range models {
					models[i] = fmt.Sprintf("model-%02d", i)
				}
				config := budgetConfig(nil, modelUsageRow("user", "daily", models...))
				enabled := true
				config.ModelUsageTrackingEnabled = &enabled
				view, err := SaveChannelPeriodPolicy(context.Background(), channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
				require.NoError(b, err)
				for _, name := range models {
					require.NoError(b, RecordChannelUserModelQuotaUsage(context.Background(), channel.Id, 7, 1, name))
				}
				res, err := resolveChannelBudgetRows(buildChannelBudgetPlan(view), now, "")
				require.NoError(b, err)
				counter := newChannelBudgetCounter(channel.Id, channelBudgetRow{ChannelBudgetRow: view.Config.Budgets[0]}, res)
				_, _, _, err = readChannelBudgetCounter(context.Background(), counter, 7, now)
				require.NoError(b, err)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, _, _, err := readChannelBudgetCounter(context.Background(), counter, 7, now); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkChannelModelUsageStorage 使用相同的 100 用户 × 20 模型结算序列，报告真实 Redis 字节及键数。
func BenchmarkChannelModelUsageStorage(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		b.Run(fmt.Sprintf("enabled=%t", enabled), func(b *testing.B) {
			now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
			channel := setupChannelModelUsageBenchmark(b, &now, true)
			ctx := context.Background()
			config := budgetConfig(nil)
			config.ModelUsageTrackingEnabled = &enabled
			_, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
			require.NoError(b, err)
			require.NoError(b, channelBudgetUsageAddScript.Load(ctx, common.RDB).Err())
			require.NoError(b, common.RDB.ConfigResetStat(ctx).Err())
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for user := 1; user <= 100; user++ {
					for m := 0; m < 20; m++ {
						require.NoError(b, RecordChannelUserModelQuotaUsage(ctx, channel.Id, user, 500, fmt.Sprintf("model-%02d", m)))
					}
				}
			}
			b.StopTimer()
			keys, err := common.RDB.Keys(ctx, "channel_*quota*{80}*").Result()
			require.NoError(b, err)
			models, err := common.RDB.Keys(ctx, "channel_model_usage:{80}:*").Result()
			require.NoError(b, err)
			keys = append(keys, models...)
			bytes := int64(0)
			for _, key := range keys {
				size, err := common.RDB.MemoryUsage(ctx, key).Result()
				require.NoError(b, err)
				bytes += size
			}
			b.ReportMetric(float64(len(keys)), "keys")
			b.ReportMetric(float64(bytes), "redis-bytes")
			stats, err := common.RDB.Info(ctx, "commandstats").Result()
			require.NoError(b, err)
			b.Log(stats)
		})
	}
}
