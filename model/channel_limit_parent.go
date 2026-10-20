package model

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidChannelLimitParent  = errors.New("父渠道关联无效：仅支持一级关联，不能关联自身或不存在的渠道")
	ErrChannelLimitParentConflict = errors.New("父渠道关联已变化，请刷新后重试")
	ErrChannelLimitParentInUse    = errors.New("渠道仍有子渠道关联，请先解除关联")
)

// ChannelLimitParent 保存单个渠道的限制继承关系，解除后保留版本以防旧请求覆盖。
// @return 渠道、父渠道、版本及管理审计元数据。
type ChannelLimitParent struct {
	ChannelId       int   `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	ParentChannelId int   `json:"parent_channel_id" gorm:"index"`
	Revision        int   `json:"revision"`
	UpdatedAt       int64 `json:"updated_at" gorm:"type:bigint"`
	UpdatedBy       int   `json:"updated_by"`
}

// ChannelLimitParentOption 是不含上游凭据的父渠道候选。
// @return 渠道的 ID、名称及物理启用状态。
type ChannelLimitParentOption struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status int    `json:"status"`
}

// GetChannelLimitParent 读取权威关系，缺少记录表示独立渠道。
// @param ctx 查询上下文。
// @param channelID 物理渠道 ID。
// @return 当前关系；不存在时返回零版本独立关系，存储故障返回错误。
func GetChannelLimitParent(ctx context.Context, channelID int) (ChannelLimitParent, error) {
	value := ChannelLimitParent{ChannelId: channelID}
	if DB == nil || channelID <= 0 {
		return value, gorm.ErrInvalidData
	}
	err := DB.WithContext(ctx).Where("channel_id = ?", channelID).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ChannelLimitParent{ChannelId: channelID}, nil
	}
	return value, err
}

// ReplaceChannelLimitParent 按版本原子修改一级父子关系，不改任何限制或历史用量。
// @param ctx 请求上下文。
// @param value 待写关系，Revision 是页面读取的旧版本。
// @return 校验、版本冲突或数据库错误；成功后回写新版本。
func ReplaceChannelLimitParent(ctx context.Context, value *ChannelLimitParent) error {
	if DB == nil || value == nil || value.ChannelId <= 0 || value.ParentChannelId < 0 || value.ChannelId == value.ParentChannelId || value.Revision < 0 || value.UpdatedBy <= 0 {
		return ErrInvalidChannelLimitParent
	}
	var saved ChannelLimitParent
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids := []int{value.ChannelId}
		if value.ParentChannelId > 0 {
			ids = append(ids, value.ParentChannelId)
		}
		sort.Ints(ids)
		var channels []Channel
		if err := lockForUpdate(tx).Select("id").Where("id IN ?", ids).Order("id ASC").Find(&channels).Error; err != nil {
			return err
		}
		if len(channels) != len(ids) {
			return ErrInvalidChannelLimitParent
		}
		var current ChannelLimitParent
		err := tx.Where("channel_id = ?", value.ChannelId).First(&current).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if current.Revision != value.Revision {
			return ErrChannelLimitParentConflict
		}
		if value.ParentChannelId > 0 {
			var count int64
			// 两端渠道行锁使“父变子”和“子挂父”竞争不能同时通过拓扑校验。
			if err := tx.Model(&ChannelLimitParent{}).Where("(channel_id = ? AND parent_channel_id > 0) OR parent_channel_id = ?", value.ParentChannelId, value.ChannelId).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrInvalidChannelLimitParent
			}
		}
		saved = *value
		saved.Revision++
		saved.UpdatedAt = time.Now().Unix()
		if current.Revision == 0 {
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&saved)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrChannelLimitParentConflict
			}
			return nil
		}
		result := tx.Model(&ChannelLimitParent{}).Where("channel_id = ? AND revision = ?", value.ChannelId, value.Revision).Updates(map[string]any{
			"parent_channel_id": saved.ParentChannelId, "revision": saved.Revision, "updated_at": saved.UpdatedAt, "updated_by": saved.UpdatedBy,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrChannelLimitParentConflict
		}
		return nil
	})
	if err == nil {
		*value = saved
	}
	return err
}

// ListChannelLimitParentOptions 分页查询顶层渠道，不读取密钥或完整渠道配置。
// @param ctx 请求上下文。
// @param channelID 需要排除的当前渠道。
// @param keyword 渠道名称或数字 ID。
// @param offset 分页偏移。
// @param limit 分页大小。
// @return 安全候选、总数和数据库错误。
func ListChannelLimitParentOptions(ctx context.Context, channelID int, keyword string, offset, limit int) ([]ChannelLimitParentOption, int64, error) {
	items := []ChannelLimitParentOption{}
	if DB == nil {
		return items, 0, gorm.ErrInvalidDB
	}
	children := DB.Model(&ChannelLimitParent{}).Select("channel_id").Where("parent_channel_id > 0")
	query := DB.WithContext(ctx).Model(&Channel{}).Where("id <> ? AND id NOT IN (?)", channelID, children)
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		if id, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("id = ? OR name LIKE ?", id, "%"+keyword+"%")
		} else {
			query = query.Where("name LIKE ?", "%"+keyword+"%")
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return items, 0, err
	}
	err := query.Select("id", "name", "status").Order("id ASC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

// protectChannelLimitParentDeletion 在删除事务内锁定渠道并阻止删除仍有子渠道的父渠道。
func protectChannelLimitParentDeletion(tx *gorm.DB, ids []int) error {
	var channels []Channel
	if err := lockForUpdate(tx).Select("id").Where("id IN ?", ids).Order("id ASC").Find(&channels).Error; err != nil {
		return err
	}
	var count int64
	if err := tx.Model(&ChannelLimitParent{}).Where("parent_channel_id IN ?", ids).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrChannelLimitParentInUse
	}
	return tx.Where("channel_id IN ?", ids).Delete(&ChannelLimitParent{}).Error
}
