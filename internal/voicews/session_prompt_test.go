package voicews

import (
	"strings"
	"testing"
	"time"

	"github.com/agendash/AgenSense/internal/provider"
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
		"Recent assistant speech",
		"好的，我会继续监听。",
		"Raw latest ASR",
		"帮我打开设置",
		"ignore the echo",
	}
	for _, check := range checks {
		if !strings.Contains(got, check) {
			t.Fatalf("recentSpeechPrompt missing %q in:\n%s", check, got)
		}
	}
}

func TestVoiceAssistantPromptOmitsSharedSystemPromptMetadata(t *testing.T) {
	t.Parallel()

	got := voiceAssistantPrompt(service.VoiceAssistantMetadata{
		Contract: "universal_voice_layer_v1",
		Metadata: map[string]any{
			"shared_system_prompt": voiceReplySystemPrompt,
			"mcp_tools":            []any{"capture_text"},
		},
	})
	if strings.Contains(got, "shared_system_prompt") || strings.Contains(got, voiceReplySystemPrompt) {
		t.Fatalf("voiceAssistantPrompt leaked shared system prompt:\n%s", got)
	}
	if !strings.Contains(got, "capture_text") {
		t.Fatalf("voiceAssistantPrompt dropped unrelated metadata:\n%s", got)
	}
}

func TestSharedSystemPromptSkipsDuplicateBuiltinPrompt(t *testing.T) {
	t.Parallel()

	got := sharedSystemPrompt(service.VoiceAssistantMetadata{
		Metadata: map[string]any{
			"shared_system_prompt": " \n" + voiceReplySystemPrompt + "\n",
		},
	})
	if got != "" {
		t.Fatalf("sharedSystemPrompt duplicate = %q, want empty", got)
	}
}

func TestCurrentDatePromptUsesSessionDate(t *testing.T) {
	t.Parallel()

	got := currentDatePrompt(time.Date(2026, 6, 22, 15, 45, 0, 0, time.FixedZone("CST", 8*3600)))
	if !strings.Contains(got, "2026-06-22") {
		t.Fatalf("currentDatePrompt = %q, want date", got)
	}
}

func TestMostlyRecentSpeechEchoSuppressesRecentAssistantOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	s := &session{
		now: func() time.Time { return now },
		recentSpeech: []recentSpeechText{
			{text: "好的，我会继续监听。", at: now},
			{text: "现在可以继续说。", at: now},
		},
	}

	match, ok := s.mostlyRecentSpeechEcho("好的我会继续聆听，现在可以继续说")
	if !ok {
		t.Fatalf("expected recent assistant speech echo match, best=%+v", match)
	}
	if match.score < echoSimilarityThreshold {
		t.Fatalf("match score = %.2f, want >= %.2f", match.score, echoSimilarityThreshold)
	}
}

func TestMostlyRecentSpeechEchoAllowsNewIntentAfterEcho(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	s := &session{
		now: func() time.Time { return now },
		recentSpeech: []recentSpeechText{
			{text: "好的，我会继续监听。", at: now},
		},
	}

	if match, ok := s.mostlyRecentSpeechEcho("好的，我会继续监听，帮我打开设置"); ok {
		t.Fatalf("mixed echo and new intent was suppressed, match=%+v", match)
	}
}

func TestMostlyRecentSpeechEchoIgnoresExpiredSpeech(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	s := &session{
		now: func() time.Time { return now },
		recentSpeech: []recentSpeechText{
			{text: "好的，我会继续监听。", at: now.Add(-recentSpeechWindow - time.Second)},
		},
	}

	if match, ok := s.mostlyRecentSpeechEcho("好的，我会继续监听。"); ok {
		t.Fatalf("expired speech was suppressed, match=%+v", match)
	}
}

func TestPlaybackEchoGuardCoversEstimatedTTSPlayback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	s := &session{now: func() time.Time { return now }}
	format := provider.AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1}

	until := s.extendPlaybackEchoWindow(format, 16000*2*5)
	if !until.Equal(now.Add(5*time.Second + playbackEchoGuardSlack)) {
		t.Fatalf("echo window = %s, want %s", until, now.Add(5*time.Second+playbackEchoGuardSlack))
	}
	now = now.Add(time.Second)
	if !now.Before(s.playbackEchoUntil) {
		t.Fatal("input one second after TTS send should be inside playback echo window")
	}
	now = now.Add(6 * time.Second)
	if !now.After(s.playbackEchoUntil) {
		t.Fatal("input after estimated playback should be outside playback echo window")
	}
}

func TestPlaybackEchoGuardDurationUsesMinimumForShortAudio(t *testing.T) {
	t.Parallel()

	format := provider.AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1}
	got := playbackEchoGuardDuration(format, 16000)
	want := playbackEchoGuardMin + playbackEchoGuardSlack
	if got != want {
		t.Fatalf("short playback echo duration = %s, want %s", got, want)
	}
}

func TestConversationOptionsUseModelLimitAndMetadataOverrides(t *testing.T) {
	base := conversationOptionsFromConfig(sessionConfig{
		LLMModel: "huihui-qwen3.6-35b-a3b-instruct-abliterated",
	})
	if base.contextLimitTokens != 262144 {
		t.Fatalf("contextLimitTokens = %d, want 262144", base.contextLimitTokens)
	}

	got := conversationOptionsFromConfig(sessionConfig{
		LLMModel: "small-4k-model",
		VoiceAssistant: service.VoiceAssistantMetadata{
			Metadata: map[string]any{
				"context_management": map[string]any{
					"max_context_tokens":       4096,
					"target_context_percent":   65,
					"compress_context_percent": 75,
					"output_reserve_tokens":    512,
					"recent_turns":             4,
					"idle_timeout_seconds":     42,
					"summary_rune_limit":       1200,
				},
			},
		},
	})
	if got.contextLimitTokens != 4096 || got.targetPercent != 65 || got.compressPercent != 75 {
		t.Fatalf("context options = %+v, want explicit token/percent overrides", got)
	}
	if got.outputReserveTokens != 512 || got.recentTurnLimit != 4 || got.idleTimeout != 42*time.Second || got.summaryRuneLimit != 1200 {
		t.Fatalf("context options = %+v, want explicit reserve/recent/idle/summary overrides", got)
	}
}

func TestConversationMemoryStoreResetsAfterIdle(t *testing.T) {
	store := newConversationMemoryStore()
	opts := conversationOptions{
		contextLimitTokens:  4096,
		targetPercent:       70,
		compressPercent:     80,
		outputReserveTokens: 256,
		recentTurnLimit:     6,
		idleTimeout:         time.Minute,
		summaryRuneLimit:    1200,
	}
	start := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

	store.append("session-a", conversationTurn{userText: "提醒我接孩子", assistantText: "需要几点？", at: start}, opts)
	_, turns, reset, _ := store.snapshot("session-a", start.Add(30*time.Second), opts)
	if reset || len(turns) != 1 {
		t.Fatalf("snapshot before idle reset = reset %v turns %d, want false/1", reset, len(turns))
	}

	summary, turns, reset, _ := store.snapshot("session-a", start.Add(2*time.Minute), opts)
	if !reset || summary != "" || len(turns) != 0 {
		t.Fatalf("snapshot after idle reset = reset %v summary %q turns %d, want true empty 0", reset, summary, len(turns))
	}
}

func TestConversationMemoryStoreCompressesOldTurns(t *testing.T) {
	store := newConversationMemoryStore()
	opts := conversationOptions{
		contextLimitTokens:  4096,
		targetPercent:       70,
		compressPercent:     80,
		outputReserveTokens: 256,
		recentTurnLimit:     2,
		idleTimeout:         10 * time.Minute,
		summaryRuneLimit:    1200,
	}
	start := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

	for i, text := range []string{"第一轮", "第二轮", "第三轮", "第四轮"} {
		store.append("session-a", conversationTurn{
			userText:      text,
			assistantText: "收到 " + text,
			at:            start.Add(time.Duration(i) * time.Second),
		}, opts)
	}

	summary, turns, _, _ := store.snapshot("session-a", start.Add(5*time.Second), opts)
	if !strings.Contains(summary, "第一轮") || !strings.Contains(summary, "第二轮") {
		t.Fatalf("summary = %q, want compressed old turns", summary)
	}
	if len(turns) != 2 || turns[0].userText != "第三轮" || turns[1].userText != "第四轮" {
		t.Fatalf("turns = %#v, want last two turns", turns)
	}
}
