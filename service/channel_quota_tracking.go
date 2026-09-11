package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// 数据库也故障时，保留本进程的缺口标记；恢复后的状态读取会补写共享标记。
var channelQuotaPendingGaps = struct {
	sync.Mutex
	values map[string]int64
}{values: make(map[string]int64)}

func recordChannelQuotaGap(ctx context.Context, channelID int, at int64) {
	key := fmt.Sprintf("%p:%d", model.DB, channelID)
	channelQuotaPendingGaps.Lock()
	channelQuotaPendingGaps.values[key] = max(channelQuotaPendingGaps.values[key], at)
	channelQuotaPendingGaps.Unlock()
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := model.RecordChannelQuotaGap(opCtx, channelID, at); err != nil {
		common.SysError(fmt.Sprintf("记录渠道计数缺口失败: channel_id=%d error=%s", channelID, common.LocalLogPreview(err.Error())))
		return
	}
	channelQuotaPendingGaps.Lock()
	if channelQuotaPendingGaps.values[key] <= at {
		delete(channelQuotaPendingGaps.values, key)
	}
	channelQuotaPendingGaps.Unlock()
}

func getChannelQuotaGap(ctx context.Context, channelID int) (int64, error) {
	key := fmt.Sprintf("%p:%d", model.DB, channelID)
	channelQuotaPendingGaps.Lock()
	pending := channelQuotaPendingGaps.values[key]
	channelQuotaPendingGaps.Unlock()
	if pending > 0 {
		recordChannelQuotaGap(ctx, channelID, pending)
	}
	persisted, err := model.GetChannelQuotaGap(ctx, channelID)
	return max(pending, persisted), err
}
