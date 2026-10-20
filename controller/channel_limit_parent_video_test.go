package controller

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelLimitParentVideoLookupRejectsSharedConcurrency 在视频结果查询前检查父桶，关联损坏时失败关闭。
func TestChannelLimitParentVideoLookupRejectsSharedConcurrency(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		for _, broken := range []bool{false, true} {
			name := constant.GetChannelTypeName(channelType)
			if broken {
				name += "/broken-parent"
			}
			t.Run(name, func(t *testing.T) {
				db := setupChannelUserLimitsTestDB(t)
				previousMemory := common.MemoryCacheEnabled
				common.MemoryCacheEnabled = false
				t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory })
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					_, _ = w.Write([]byte(`{"done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://video.example/result.mp4"}}]}}}`))
				}))
				t.Cleanup(upstream.Close)
				service.InitHttpClient()
				parent := &model.Channel{Id: 79501, Name: "视频限制父渠道", UserConcurrencyLimit: common.GetPointer(4)}
				child := &model.Channel{Id: 79502, Name: "视频实际上游", Type: channelType, Key: "local-test-key", BaseURL: &upstream.URL, UserConcurrencyLimit: common.GetPointer(1)}
				require.NoError(t, db.Create(parent).Error)
				require.NoError(t, db.Create(child).Error)
				if broken {
					require.NoError(t, db.Create(&model.ChannelLimitParent{ChannelId: child.Id, ParentChannelId: 999999, Revision: 1, UpdatedBy: 1}).Error)
				} else {
					require.NoError(t, service.SaveChannelLimitParent(t.Context(), &model.ChannelLimitParent{ChannelId: child.Id, ParentChannelId: parent.Id, UpdatedBy: 1}))
				}
				leases := make([]*service.ChannelUserConcurrencyLease, 0, 4)
				for i := 0; i < 4; i++ {
					lease, err := service.AcquireChannelUserConcurrency(t.Context(), parent.Id, 79503, 4, nil)
					require.NoError(t, err)
					leases = append(leases, lease)
					t.Cleanup(func() { require.NoError(t, lease.Release(t.Context())) })
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/local-task/content", nil)
				common.SetContextKey(c, constant.ContextKeyUserId, 79503)
				task := &model.Task{ChannelId: child.Id, TaskID: taskcommon.EncodeLocalTaskID("operations/parent-video-test"), PrivateData: model.TaskPrivateData{Key: "local-test-key"}}
				var lookupErr error
				if channelType == constant.ChannelTypeGemini {
					_, lookupErr = getGeminiVideoURL(c, child, task, "local-test-key")
				} else {
					_, lookupErr = getVertexVideoURL(c, child, task)
				}
				var apiErr *types.NewAPIError
				require.ErrorAs(t, lookupErr, &apiErr)
				if broken {
					assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
				} else {
					assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
					assert.Equal(t, types.ErrorCodeChannelUserConcurrencyExceeded, apiErr.GetErrorCode())
				}
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.Zero(t, calls.Load())
				if !broken && channelType == constant.ChannelTypeGemini {
					require.NoError(t, leases[0].Release(t.Context()))
					url, err := getGeminiVideoURL(c, child, task, "local-test-key")
					require.NoError(t, err)
					assert.Equal(t, "https://video.example/result.mp4?key=local-test-key", url)
					assert.EqualValues(t, 1, calls.Load())
					usage, _, err := service.GetChannelUserConcurrencyUsage(t.Context(), parent.Id, 79503)
					require.NoError(t, err)
					assert.Equal(t, 3, usage)
					childUsage, _, err := service.GetChannelUserConcurrencyUsage(t.Context(), child.Id, 79503)
					require.NoError(t, err)
					assert.Zero(t, childUsage)
				}
			})
		}
	}
}
