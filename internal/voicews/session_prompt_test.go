package voicews

import (
	"strings"
	"testing"
	"time"

	"github.com/agendash/AgenSense/internal/service"
)

func TestVoiceReplySystemPromptGuidesASRIntentNormalization(t *testing.T) {
	t.Parallel()

	checks := []string{
		"raw ASR transcript",
		"Silently infer the user's real intent",
		"acoustic echo",
		"preserve names, numbers, commands, file paths",
		"mostly echo or too ambiguous",
	}
	for _, check := range checks {
		if !strings.Contains(voiceReplySystemPrompt, check) {
			t.Fatalf("voiceReplySystemPrompt missing %q", check)
		}
	}
}

func TestMCPToolsFromVoiceAssistantMetadata(t *testing.T) {
	t.Parallel()

	tools := mcpToolsFromVoiceAssistant(service.VoiceAssistantMetadata{
		UIContext: map[string]any{
			"available_mcp_tools": []any{
				"joyce.capture_text",
				map[string]any{"name": "joyce.create_reminder_candidate"},
			},
		},
		Metadata: map[string]any{
			"mcp_tools": []any{"joyce.capture_text", "joyce.search_records"},
		},
	})

	want := []string{"joyce.capture_text", "joyce.search_records", "joyce.create_reminder_candidate"}
	if len(tools) != len(want) {
		t.Fatalf("tools = %#v, want %#v", tools, want)
	}
	for i := range want {
		if tools[i] != want[i] {
			t.Fatalf("tools = %#v, want %#v", tools, want)
		}
	}
}

func TestParseMCPProposal(t *testing.T) {
	t.Parallel()

	got, err := parseMCPProposal(
		"```json\n{\"tool_name\":\"joyce.create_reminder_candidate\",\"arguments\":{\"title\":\"接孩子\"},\"confidence\":0.82,\"requires_confirmation\":true,\"reason\":\"contains reminder\"}\n```",
		"今天下午四点提醒我接孩子",
		[]string{"joyce.capture_text", "joyce.create_reminder_candidate"},
	)
	if err != nil {
		t.Fatalf("parseMCPProposal() error = %v", err)
	}
	if got.ToolName != "joyce.create_reminder_candidate" {
		t.Fatalf("tool = %q", got.ToolName)
	}
	if got.Arguments["raw_text"] != "今天下午四点提醒我接孩子" {
		t.Fatalf("expected raw_text argument, got %#v", got.Arguments)
	}
	if !got.RequiresConfirmation {
		t.Fatal("expected confirmation flag")
	}
}

func TestRecentSpeechPromptFramesEchoAndRawASR(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	s := &session{
		now: func() time.Time { return now },
		recentSpeech: []recentSpeechText{
			{text: "好的，我会继续监听。", at: now},
		},
	}

	got := s.recentSpeechPrompt("好的，我会继续监听。帮我打开设置")
	checks := []string{
		"Assistant speech already played:",
		"好的，我会继续监听。",
		"Raw latest ASR transcript:",
		"帮我打开设置",
		"discard only the echoed portion",
		"ask one short clarification",
	}
	for _, check := range checks {
		if !strings.Contains(got, check) {
			t.Fatalf("recentSpeechPrompt missing %q in:\n%s", check, got)
		}
	}
}
