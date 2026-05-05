package voicelang

import "strings"

const (
	Auto               = ""
	SimplifiedChinese  = "zh-Hans"
	TraditionalChinese = "zh-Hant"
	English            = "en"
)

// Normalize converts client/user-facing language labels into stable wire values.
// An empty return value means automatic language selection.
func Normalize(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return Auto
	}
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, "_", "-"), " ", "-"))
	switch key {
	case "auto", "automatic", "follow", "follow-input", "same-as-input", "same-language":
		return Auto
	case "zh", "zh-cn", "zh-sg", "zh-hans", "zh-hans-cn", "chinese", "simplified", "simplified-chinese", "mandarin", "简体", "简体中文", "簡體", "簡體中文":
		return SimplifiedChinese
	case "zh-tw", "zh-hk", "zh-mo", "zh-hant", "zh-hant-tw", "traditional", "traditional-chinese", "繁体", "繁體", "繁体中文", "繁體中文":
		return TraditionalChinese
	case "en", "en-us", "en-gb", "english":
		return English
	default:
		return Auto
	}
}

func FromMetadata(metadata map[string]any) string {
	for _, key := range []string{
		"response_language",
		"voice_response_language",
		"reply_language",
		"language",
		"locale",
	} {
		if value, ok := metadata[key]; ok {
			if normalized := Normalize(anyString(value)); normalized != Auto {
				return normalized
			}
			if isExplicitAuto(value) {
				return Auto
			}
		}
	}
	return Auto
}

func Instruction(preference string, latestUserText string) string {
	switch Normalize(preference) {
	case SimplifiedChinese:
		return "Response language policy: Reply in Simplified Chinese. Keep product names, code identifiers, commands, paths, and quoted user text unchanged."
	case TraditionalChinese:
		return "Response language policy: Reply in Traditional Chinese. Keep product names, code identifiers, commands, paths, and quoted user text unchanged."
	case English:
		return "Response language policy: Reply in English. Keep product names, code identifiers, commands, paths, and quoted user text unchanged."
	default:
		if containsCJK(latestUserText) {
			return "Response language policy: Reply in the same broad language as the user's latest utterance. For Chinese replies, use Simplified Chinese by default; use Traditional Chinese only if the user explicitly asks for Traditional Chinese or the client config sets zh-Hant. If the utterance mixes Chinese and English, answer primarily in Simplified Chinese while preserving product names, code identifiers, commands, paths, and quoted user text."
		}
		return "Response language policy: Reply in the same language as the user's latest utterance. If the user mixes Chinese and English, match the dominant language. For Chinese replies, use Simplified Chinese by default. Keep product names, code identifiers, commands, paths, and quoted user text unchanged."
	}
}

func Label(preference string, latestUserText string) string {
	switch Normalize(preference) {
	case SimplifiedChinese:
		return SimplifiedChinese
	case TraditionalChinese:
		return TraditionalChinese
	case English:
		return English
	default:
		if containsCJK(latestUserText) {
			return "auto:zh-Hans"
		}
		return "auto"
	}
}

func containsCJK(text string) bool {
	for _, r := range text {
		switch {
		case r >= '\u3400' && r <= '\u4dbf':
			return true
		case r >= '\u4e00' && r <= '\u9fff':
			return true
		case r >= '\uf900' && r <= '\ufaff':
			return true
		}
	}
	return false
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}

func isExplicitAuto(value any) bool {
	raw := strings.TrimSpace(anyString(value))
	if raw == "" {
		return false
	}
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(raw, "_", "-"), " ", "-"))
	return key == "auto" || key == "automatic" || key == "follow" || key == "follow-input" || key == "same-as-input" || key == "same-language"
}
