package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetChannelLimitParent 返回物理渠道的继承来源与只读状态。
// @param c 已授权的渠道读取请求。
// @return 无，写入安全关系视图。
func GetChannelLimitParent(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		respondChannelLimitParentError(c, model.ErrInvalidChannelLimitParent)
		return
	}
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = model.ErrInvalidChannelLimitParent
		}
		respondChannelLimitParentError(c, err)
		return
	}
	relation, err := model.GetChannelLimitParent(c, channelID)
	if err != nil {
		respondChannelLimitParentError(c, err)
		return
	}
	view := dto.ChannelLimitParentView{ChannelID: channelID, ParentChannelID: relation.ParentChannelId, Revision: relation.Revision, ReadOnly: relation.ParentChannelId > 0}
	view.ConcurrencyLimit = channel.GetUserConcurrencyLimit()
	if relation.ParentChannelId > 0 {
		parent, err := model.GetChannelById(relation.ParentChannelId, false)
		if err != nil {
			respondChannelLimitParentError(c, err)
			return
		}
		view.Parent = &dto.ChannelLimitParentSummary{ID: parent.Id, Name: parent.Name}
		view.ConcurrencyLimit = parent.GetUserConcurrencyLimit()
	}
	common.ApiSuccess(c, view)
}

// GetChannelLimitParentOptions 返回分页的安全父渠道候选。
// @param c 含 keyword/p/page_size 的管理请求。
// @return 无，写入顶层渠道摘要与分页。
func GetChannelLimitParentOptions(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		respondChannelLimitParentError(c, model.ErrInvalidChannelLimitParent)
		return
	}
	page, ok := getChannelUserLimitPage(c)
	if !ok {
		return
	}
	items, total, err := model.ListChannelLimitParentOptions(c, channelID, c.Query("keyword"), page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		respondChannelLimitParentError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "page": page.GetPage(), "page_size": page.GetPageSize()})
}

// SetChannelLimitParent 保存或解除关联，不改写父子原有配置与历史计数。
// @param c 含父渠道与预期版本的管理请求。
// @return 无，写入新关系视图或明确的版本冲突。
func SetChannelLimitParent(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		respondChannelLimitParentError(c, model.ErrInvalidChannelLimitParent)
		return
	}
	var input dto.ChannelLimitParentInput
	if !bindChannelPeriodJSON(c, &input) {
		return
	}
	if input.ParentChannelID == nil || input.ExpectedRevision == nil {
		respondChannelLimitParentError(c, model.ErrInvalidChannelLimitParent)
		return
	}
	before, err := model.GetChannelLimitParent(c, channelID)
	if err != nil {
		respondChannelLimitParentError(c, err)
		return
	}
	relation := model.ChannelLimitParent{ChannelId: channelID, ParentChannelId: *input.ParentChannelID, Revision: *input.ExpectedRevision, UpdatedBy: common.GetContextKeyInt(c, constant.ContextKeyUserId)}
	if err := service.SaveChannelLimitParent(c, &relation); err != nil {
		respondChannelLimitParentError(c, err)
		return
	}
	recordManageAudit(c, "channel.limit_parent_set", map[string]any{"channel_id": channelID, "before": before, "after": relation})
	GetChannelLimitParent(c)
}

func respondChannelLimitParentError(c *gin.Context, err error) {
	status, code, message := http.StatusServiceUnavailable, "channel_limit_parent_unavailable", "渠道限制关联暂不可用"
	switch {
	case errors.Is(err, model.ErrInvalidChannelLimitParent):
		status, code, message = http.StatusBadRequest, "invalid_channel_limit_parent", err.Error()
	case errors.Is(err, model.ErrChannelLimitParentConflict):
		status, code, message = http.StatusConflict, "channel_limit_parent_conflict", err.Error()
	case errors.Is(err, service.ErrChannelLimitParentReadOnly):
		status, code, message = http.StatusConflict, "channel_limit_parent_read_only", err.Error()
	default:
		logger.LogError(c, fmt.Sprintf("渠道限制关联操作失败: channel_id=%s error=%s", c.Param("id"), common.LocalLogPreview(err.Error())))
	}
	c.JSON(status, gin.H{"success": false, "message": message, "code": code})
}

// channelLimitRequestedID 保留读取子渠道时的路径身份，计数与配置使用父渠道。
func channelLimitRequestedID(c *gin.Context, channel *model.Channel) int {
	if id := c.GetInt("channel_limit_requested_id"); id > 0 {
		return id
	}
	return channel.Id
}
