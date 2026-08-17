package service

import "strings"

type workBuddyVisionAssistTextRange struct {
	start int
	end   int
}

type workBuddySystemReminderTag struct {
	start       int
	end         int
	closing     bool
	selfClosing bool
	userContext bool
}

type workBuddySystemReminderFrame struct {
	tag    workBuddySystemReminderTag
	nested bool
}

func filterWorkBuddyVisionAssistUserMessage(raw string) (string, bool) {
	tags := scanWorkBuddySystemReminderTags(raw)
	if len(tags) == 0 {
		return raw, false
	}

	frames := make([]workBuddySystemReminderFrame, 0, 1)
	ranges := make([]workBuddyVisionAssistTextRange, 0, 1)
	for _, tag := range tags {
		if !tag.closing {
			if len(frames) > 0 {
				// 嵌套提醒的边界语义不可靠，整组标签均保留原文，避免越界删除用户内容。
				for index := range frames {
					frames[index].nested = true
				}
			}
			if tag.selfClosing {
				continue
			}
			frames = append(frames, workBuddySystemReminderFrame{
				tag:    tag,
				nested: len(frames) > 0,
			})
			continue
		}
		if len(frames) == 0 {
			continue
		}
		frame := frames[len(frames)-1]
		frames = frames[:len(frames)-1]
		if frame.nested || len(frames) > 0 || !frame.tag.userContext {
			continue
		}
		ranges = append(ranges, workBuddyVisionAssistTextRange{start: frame.tag.start, end: tag.end})
	}
	if len(ranges) == 0 {
		return raw, false
	}

	parts := make([]string, 0, len(ranges)+1)
	previousEnd := 0
	for _, textRange := range ranges {
		if part := strings.TrimSpace(raw[previousEnd:textRange.start]); part != "" {
			parts = append(parts, part)
		}
		previousEnd = textRange.end
	}
	if part := strings.TrimSpace(raw[previousEnd:]); part != "" {
		parts = append(parts, part)
	}
	return strings.Join(parts, "\n"), true
}

func scanWorkBuddySystemReminderTags(raw string) []workBuddySystemReminderTag {
	tags := make([]workBuddySystemReminderTag, 0, 2)
	for searchStart := 0; searchStart < len(raw); {
		relativeStart := strings.IndexByte(raw[searchStart:], '<')
		if relativeStart < 0 {
			break
		}
		start := searchStart + relativeStart
		tag, ok := parseWorkBuddySystemReminderTag(raw, start)
		if !ok {
			searchStart = start + 1
			continue
		}
		tags = append(tags, tag)
		searchStart = tag.end
	}
	return tags
}

func parseWorkBuddySystemReminderTag(raw string, start int) (workBuddySystemReminderTag, bool) {
	if start < 0 || start >= len(raw) || raw[start] != '<' {
		return workBuddySystemReminderTag{}, false
	}
	i := start + 1
	for i < len(raw) && isWorkBuddyMarkerSpace(raw[i]) {
		i++
	}
	closing := false
	if i < len(raw) && raw[i] == '/' {
		closing = true
		i++
		for i < len(raw) && isWorkBuddyMarkerSpace(raw[i]) {
			i++
		}
	}
	nameStart := i
	for i < len(raw) && isWorkBuddyMarkerNameByte(raw[i]) {
		i++
	}
	if nameStart == i || !strings.EqualFold(raw[nameStart:i], "system-reminder") {
		return workBuddySystemReminderTag{}, false
	}
	if i < len(raw) && !isWorkBuddyMarkerSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' {
		return workBuddySystemReminderTag{}, false
	}
	tagEnd := findWorkBuddyMarkerTagEnd(raw, i)
	if tagEnd < 0 {
		return workBuddySystemReminderTag{}, false
	}
	inside := raw[i:tagEnd]
	tag := workBuddySystemReminderTag{start: start, end: tagEnd + 1, closing: closing}
	if closing {
		if strings.TrimSpace(inside) != "" {
			return workBuddySystemReminderTag{}, false
		}
		return tag, true
	}

	inside = strings.TrimSpace(inside)
	if strings.HasSuffix(inside, "/") {
		tag.selfClosing = true
		inside = strings.TrimSpace(inside[:len(inside)-1])
	}
	userContext, valid := hasWorkBuddyUserContextAttribute(inside)
	if !valid {
		return workBuddySystemReminderTag{}, false
	}
	tag.userContext = userContext
	return tag, true
}

func findWorkBuddyMarkerTagEnd(raw string, start int) int {
	var quote byte
	for i := start; i < len(raw); i++ {
		if quote != 0 {
			if raw[i] == quote {
				quote = 0
			}
			continue
		}
		switch raw[i] {
		case '\'', '"':
			quote = raw[i]
		case '<':
			return -1
		case '>':
			return i
		}
	}
	return -1
}

func hasWorkBuddyUserContextAttribute(attributes string) (bool, bool) {
	userContext := false
	for i := 0; i < len(attributes); {
		for i < len(attributes) && isWorkBuddyMarkerSpace(attributes[i]) {
			i++
		}
		if i >= len(attributes) {
			break
		}
		nameStart := i
		for i < len(attributes) && isWorkBuddyAttributeNameByte(attributes[i]) {
			i++
		}
		if nameStart == i {
			return false, false
		}
		name := attributes[nameStart:i]
		for i < len(attributes) && isWorkBuddyMarkerSpace(attributes[i]) {
			i++
		}
		if i >= len(attributes) || attributes[i] != '=' {
			continue
		}
		i++
		for i < len(attributes) && isWorkBuddyMarkerSpace(attributes[i]) {
			i++
		}
		if i >= len(attributes) {
			return false, false
		}

		value := ""
		if attributes[i] == '\'' || attributes[i] == '"' {
			quote := attributes[i]
			i++
			valueStart := i
			for i < len(attributes) && attributes[i] != quote {
				i++
			}
			if i >= len(attributes) {
				return false, false
			}
			value = attributes[valueStart:i]
			i++
		} else {
			valueStart := i
			for i < len(attributes) && !isWorkBuddyMarkerSpace(attributes[i]) {
				i++
			}
			if valueStart == i {
				return false, false
			}
			value = attributes[valueStart:i]
		}
		if normalizeWorkBuddyMarkerPart(name) == "data-role" && normalizeWorkBuddyMarkerPart(value) == "user-context" {
			userContext = true
		}
	}
	return userContext, true
}

func normalizeWorkBuddyMarkerPart(value string) string {
	value = strings.TrimSpace(value)
	buffer := make([]byte, len(value))
	for i := range value {
		current := value[i]
		switch {
		case current >= 'A' && current <= 'Z':
			buffer[i] = current + ('a' - 'A')
		case current == '_':
			buffer[i] = '-'
		default:
			buffer[i] = current
		}
	}
	return string(buffer)
}

func isWorkBuddyMarkerSpace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func isWorkBuddyMarkerNameByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '-' || value == '_' || value == ':'
}

func isWorkBuddyAttributeNameByte(value byte) bool {
	return isWorkBuddyMarkerNameByte(value) || value == '.'
}
