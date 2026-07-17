package controller

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

func sanitizeChannelForResponse(channel *model.Channel) *model.Channel {
	if channel == nil {
		return nil
	}
	clone := *channel
	clearChannelInfo(&clone)
	sanitizeChannelWebSearchSetting(&clone)
	return &clone
}

func sanitizeChannelsForResponse(channels []*model.Channel) []*model.Channel {
	result := make([]*model.Channel, 0, len(channels))
	for _, channel := range channels {
		result = append(result, sanitizeChannelForResponse(channel))
	}
	return result
}

func parseChannelSettingRecord(setting *string) (map[string]any, error) {
	record := make(map[string]any)
	if setting == nil || strings.TrimSpace(*setting) == "" {
		return record, nil
	}
	if err := common.Unmarshal([]byte(*setting), &record); err != nil {
		return nil, err
	}
	if record == nil {
		record = make(map[string]any)
	}
	return record, nil
}

func parseWebSearchSettingsFromRecord(record map[string]any) (dto.ChannelWebSearchSettings, bool, error) {
	raw, ok := record["web_search"]
	if !ok || raw == nil {
		return dto.ChannelWebSearchSettings{}, false, nil
	}
	rawBytes, err := common.Marshal(raw)
	if err != nil {
		return dto.ChannelWebSearchSettings{}, false, err
	}
	var settings dto.ChannelWebSearchSettings
	if err := common.Unmarshal(rawBytes, &settings); err != nil {
		return dto.ChannelWebSearchSettings{}, false, err
	}
	settings.Normalize()
	return settings, true, nil
}

func setWebSearchSettingsToRecord(record map[string]any, settings dto.ChannelWebSearchSettings) error {
	settings.APIKeyConfigured = false
	settings.ClearAPIKey = false
	rawBytes, err := common.Marshal(settings)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := common.Unmarshal(rawBytes, &raw); err != nil {
		return err
	}
	record["web_search"] = raw
	return nil
}

func applyChannelSettingRecord(channel *model.Channel, record map[string]any) error {
	settingBytes, err := common.Marshal(record)
	if err != nil {
		return err
	}
	channel.Setting = common.GetPointer(string(settingBytes))
	return nil
}

func sanitizeChannelWebSearchSetting(channel *model.Channel) {
	record, err := parseChannelSettingRecord(channel.Setting)
	if err != nil {
		return
	}
	settings, exists, err := parseWebSearchSettingsFromRecord(record)
	if err != nil || !exists {
		return
	}
	configured := settings.HasAPIKey() || settings.APIKeyConfigured
	settings.APIKey = ""
	settings.APIKeyConfigured = configured
	settings.ClearAPIKey = false
	rawBytes, err := common.Marshal(settings)
	if err != nil {
		return
	}
	var raw map[string]any
	if err := common.Unmarshal(rawBytes, &raw); err != nil {
		return
	}
	record["web_search"] = raw
	if err := applyChannelSettingRecord(channel, record); err != nil {
		return
	}
}

func normalizeChannelWebSearchForCreate(channel *model.Channel) error {
	if channel == nil {
		return fmt.Errorf("channel cannot be empty")
	}
	record, err := parseChannelSettingRecord(channel.Setting)
	if err != nil {
		return err
	}
	settings, exists, err := parseWebSearchSettingsFromRecord(record)
	if err != nil || !exists {
		return err
	}
	if settings.ClearAPIKey {
		settings.APIKey = ""
	}
	settings.Normalize()
	if err := settings.ValidateForRelay(); err != nil {
		return err
	}
	if err := setWebSearchSettingsToRecord(record, settings); err != nil {
		return err
	}
	return applyChannelSettingRecord(channel, record)
}

func mergeChannelWebSearchAPIKey(channel *model.Channel, origin *model.Channel) error {
	if channel == nil || origin == nil {
		return fmt.Errorf("channel cannot be empty")
	}
	record, err := parseChannelSettingRecord(channel.Setting)
	if err != nil {
		return err
	}
	settings, exists, err := parseWebSearchSettingsFromRecord(record)
	if err != nil || !exists {
		return err
	}
	originRecord, err := parseChannelSettingRecord(origin.Setting)
	if err != nil {
		return err
	}
	originSettings, originExists, err := parseWebSearchSettingsFromRecord(originRecord)
	if err != nil {
		return err
	}
	if settings.ClearAPIKey {
		settings.APIKey = ""
	} else if !settings.HasAPIKey() && originExists && originSettings.HasAPIKey() {
		settings.APIKey = originSettings.APIKey
	}
	settings.Normalize()
	if err := settings.ValidateForRelay(); err != nil {
		return err
	}
	if err := setWebSearchSettingsToRecord(record, settings); err != nil {
		return err
	}
	return applyChannelSettingRecord(channel, record)
}
