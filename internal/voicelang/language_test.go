package voicelang

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":                    Auto,
		"auto":                Auto,
		"zh-CN":               SimplifiedChinese,
		"zh_Hans":             SimplifiedChinese,
		"简体中文":                SimplifiedChinese,
		"zh-TW":               TraditionalChinese,
		"繁體中文":                TraditionalChinese,
		"English":             English,
		"unsupported-setting": Auto,
	}
	for input, want := range tests {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInstructionAutoUsesSimplifiedChineseForChineseInput(t *testing.T) {
	t.Parallel()

	got := Instruction(Auto, "這是一段測試，請回覆我。")
	if !strings.Contains(got, "Simplified Chinese") {
		t.Fatalf("Instruction(auto, Chinese) = %q, want Simplified Chinese policy", got)
	}
	if strings.Contains(got, "Traditional Chinese by default") {
		t.Fatalf("Instruction(auto, Chinese) = %q, should not default to Traditional Chinese", got)
	}
}

func TestInstructionHonorsExplicitPreference(t *testing.T) {
	t.Parallel()

	if got := Instruction("zh-Hant", "请回复"); !strings.Contains(got, "Traditional Chinese") {
		t.Fatalf("Instruction(zh-Hant) = %q, want Traditional Chinese", got)
	}
	if got := Instruction("en", "请回复"); !strings.Contains(got, "English") {
		t.Fatalf("Instruction(en) = %q, want English", got)
	}
}

func TestFromMetadata(t *testing.T) {
	t.Parallel()

	got := FromMetadata(map[string]any{"response_language": "zh_CN"})
	if got != SimplifiedChinese {
		t.Fatalf("FromMetadata() = %q, want %q", got, SimplifiedChinese)
	}
}
