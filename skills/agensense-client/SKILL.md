---
name: agensense-client
description: Build third-party clients and integrations on top of AgenSense. Use when implementing desktop, mobile, web, backend, hardware, automation, or agent code that calls AgenSense provider profiles, direct ASR/LLM/TTS APIs, voice WebSocket sessions, or debug trace APIs. This skill is for using AgenSense as a service, not for modifying the AgenSense server codebase.
---

# AgenSense Client Integration

Use this skill when building an integration that consumes AgenSense from another app, service, device, automation, or agent.

Do not use this skill as a maintainer guide for editing AgenSense internals. Treat AgenSense as an external service with HTTP and WebSocket contracts.

## Integration Modes

Prefer the shared service mode unless the client is a hardware/device protocol peer.

- Shared service mode: use `Authorization: Bearer <AGENSENSE_API_KEY>`, provider profiles, and direct ASR/LLM/TTS APIs.
- Realtime voice mode: use `/v1/voice/ws` for AgenDash-style microphone streaming and streamed LLM/TTS response events.
- Device compatibility mode: use `/v1/bootstrap` and `/v1/session/ws` only when implementing ESP32/M5Stack/HID-style device clients.
- Debug mode: use `/debug/traces` and `/debug/api/traces` only when the server was started with `AGENSENSE_DEBUG=true`.

## Required Inputs

Before writing client code, identify:

- `AGENSENSE_BASE_URL`, for example `http://127.0.0.1:8080`
- `AGENSENSE_API_KEY`
- provider profile id, usually `default`
- desired audio format, currently safest as `pcm_s16le`, 16 kHz, mono
- whether the client needs direct HTTP APIs, realtime voice WebSocket, or both

Never hardcode real provider API keys or user secrets into source code. Load them from environment, config, OS keychain, or the host application's secret store.

## Provider Profiles

Use provider profiles so clients do not embed provider-specific ASR/LLM/TTS logic.

List profiles:

```sh
curl -sS "$AGENSENSE_BASE_URL/v1/providers" \
  -H "Authorization: Bearer $AGENSENSE_API_KEY"
```

Register or update a profile:

```sh
curl -sS -X POST "$AGENSENSE_BASE_URL/v1/providers" \
  -H "Authorization: Bearer $AGENSENSE_API_KEY" \
  -H "content-type: application/json" \
  -d '{
    "id": "default",
    "name": "OpenAI Compatible Default",
    "asr_base_url": "http://127.0.0.1:8081/v1",
    "asr_model": "whisper-1",
    "llm_base_url": "http://127.0.0.1:8081/v1",
    "llm_model": "gemma-4-e2b-it",
    "tts_base_url": "http://127.0.0.1:8081/v1",
    "tts_model": "tts-1",
    "default": true
  }'
```

Use `mock://asr`, `mock://llm`, and `mock://tts` for deterministic local tests.

## Direct APIs

Use direct APIs for text-mode apps, backend automations, one-shot transcription, or server-side speech synthesis.

Primary endpoints:

- `POST /v1/asr/transcribe`
- `POST /v1/llm/chat`
- `POST /v1/tts/synthesize`

Common request fields:

- `provider_profile_id`: profile id, optional if the namespace has a default
- `client_id`: stable caller id
- `session_id`: stable per-turn or per-conversation id
- `device_label`: human-readable client type such as `MacOS`, `Android`, `Web`, or `Backend`

LLM example:

```sh
curl -sS -X POST "$AGENSENSE_BASE_URL/v1/llm/chat" \
  -H "Authorization: Bearer $AGENSENSE_API_KEY" \
  -H "content-type: application/json" \
  -d '{
    "provider_profile_id": "default",
    "client_id": "my-client",
    "session_id": "session-001",
    "messages": [
      {"role": "system", "content": "You are concise."},
      {"role": "user", "content": "Summarize this workspace state."}
    ]
  }'
```

## Realtime Voice WebSocket

Use `GET /v1/voice/ws` when the client streams microphone PCM to AgenSense and expects streamed ASR, LLM, and TTS events back.

Session setup:

```json
{
  "type": "session.update",
  "payload": {
    "client_id": "my-client",
    "device_label": "MacOS",
    "session_id": "voice-session-001",
    "provider_profile_id": "default",
    "response_language": "auto",
    "voice_assistant": {
      "ui_context": {
        "available_mcp_tools": [
          "joyce.capture_text",
          "joyce.create_reminder_candidate"
        ]
      }
    },
    "format": {
      "codec": "pcm_s16le",
      "sample_rate_hz": 16000,
      "channels": 1
    }
  }
}
```

Input stream:

```json
{
  "type": "audio.start",
  "payload": {
    "stream_id": "input-001",
    "codec": "pcm_s16le",
    "sample_rate_hz": 16000,
    "channels": 1
  }
}
```

Then send raw PCM chunks as WebSocket binary frames. Finish with:

```json
{
  "type": "audio.stop",
  "payload": {
    "stream_id": "input-001",
    "last_seq": 25
  }
}
```

Expected server events include:

- `session.ready`
- `vad.state`
- `asr.partial`
- `asr.final`
- `llm.delta`
- `llm.done`
- `mcp.call.proposed` when the session declares MCP tools in `voice_assistant`
- `tts.start`
- binary TTS audio frames
- `tts.stop`
- `response.done`

After receiving `asr.final`, most clients should send:

```json
{
  "type": "response.create",
  "payload": {
    "text": "the final ASR transcript",
    "response_language": "auto"
  }
}
```

`response_language` supports `auto`, `zh-Hans`, `zh-Hant`, and `en`. In `auto`, Chinese replies default to Simplified Chinese unless the user explicitly requests Traditional Chinese.

## Debugging

If the server runs with `AGENSENSE_DEBUG=true`, inspect:

- `GET /debug/traces`
- `GET /debug/api/traces`
- `GET /debug/api/traces/{id}`
- `GET /debug/assets/{id}/input.wav`
- `GET /debug/assets/{id}/tts.wav`

Use debug traces to inspect captured audio, ASR text, LLM messages/deltas, TTS text, TTS audio, and timeline latency.

## Smoke Testing

Use the smoke runner to validate a client-compatible server before blaming client capture or playback code:

```sh
go run github.com/agendash/AgenSense/cmd/agensense-smoke@latest \
  -base-url "$AGENSENSE_BASE_URL" \
  -api-key "$AGENSENSE_API_KEY"
```

For local source checkouts:

```sh
go run ./cmd/agensense-smoke
```

The smoke runner can synthesize a seed TTS prompt, stream it through `/v1/voice/ws`, and verify ASR, LLM deltas, TTS binary audio, and optional debug traces.

## Client Implementation Rules

- Keep audio format explicit. Start with `pcm_s16le`, 16 kHz, mono.
- Track binary frame count and set `audio.stop.payload.last_seq` to the number of sent audio frames.
- Treat `asr.partial` as UI-only. Trigger final actions from `asr.final`.
- Cancel stale responses when the user starts a new turn.
- Do not assume provider model ids. Read or configure provider profiles.
- Keep user-facing tool execution in the client. AgenSense provides sensing/model orchestration, not application-specific UI control.
- Use mock provider profiles in automated tests to avoid depending on live model services.

## Reference Docs

- `docs/provider-api.md`
- `docs/protocol.md`
- `docs/mvp-local-runbook.md`
- `docs/localai.md`
- `docs/deployment.md`
