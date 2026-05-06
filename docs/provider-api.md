# Provider API

The provider API is the primary shared-service interface for AgenSense. It lets clients register reusable upstream model profiles and then call ASR, LLM, and TTS without embedding provider-specific code in every client.

## Authentication

Provider registry and direct inference endpoints use:

```http
Authorization: Bearer <AGENSENSE_API_KEY>
```

The API key maps to a stable internal namespace. The raw API key is not stored.

By default, AgenSense seeds the `demo-user-key` namespace with a LocalAI-oriented default provider:

- base URL: `http://127.0.0.1:8081/v1`
- ASR model: `whisper-1`
- LLM model: `hauhaucs-qwen3.6-35b-a3b-aggressive-q4-k-m`
- TTS model: `faster-qwen3-tts`

See [LocalAI setup](localai.md) for the recommended local address layout.

## Provider Profiles

Supported fields:

- `asr_base_url`
- `asr_api_key`
- `asr_model`
- `llm_base_url`
- `llm_api_key`
- `llm_model`
- `tts_base_url`
- `tts_api_key`
- `tts_model`
- `vad_base_url`
- `vad_api_key`

Provider credentials are currently persisted as plain text in the local JSON store. Treat the first implementation as local-dev or trusted single-node infrastructure.

## Register A Provider

`POST /v1/providers`

```json
{
  "id": "default",
  "name": "OpenAI Compatible Default",
  "asr_base_url": "http://127.0.0.1:8081/v1",
  "asr_api_key": "******",
  "asr_model": "whisper-1",
  "llm_base_url": "http://127.0.0.1:8081/v1",
  "llm_api_key": "******",
  "llm_model": "hauhaucs-qwen3.6-35b-a3b-aggressive-q4-k-m",
  "tts_base_url": "http://127.0.0.1:8081/v1",
  "tts_api_key": "******",
  "tts_model": "faster-qwen3-tts",
  "default": true
}
```

Behavior:

- creates the profile when `id` does not exist
- replaces the profile when `id` already exists
- makes the profile the namespace default when `default=true`

## Query Providers

`GET /v1/providers`

Returns all provider profiles in the current API-key namespace.

`GET /v1/providers/{id}`

Returns one provider profile.

`PATCH /v1/providers/{id}`

Updates one provider profile.

## Direct ASR

`POST /v1/asr/transcribe`

```json
{
  "provider_profile_id": "default",
  "client_id": "agendash-desktop",
  "device_label": "MacOS",
  "session_id": "voice-001",
  "format": {
    "codec": "pcm_s16le",
    "sample_rate_hz": 16000,
    "channels": 1
  },
  "audio_base64": "AQIDBAU="
}
```

Response:

```json
{
  "provider_profile_id": "default",
  "text": "..."
}
```

AgenSense normalizes Chinese ASR transcripts to Simplified Chinese by default. Set `AGENSENSE_ASR_CHINESE_SCRIPT=original` to keep the upstream provider output unchanged, or `zh-Hant` if a deployment intentionally wants Traditional Chinese. OpenAI-compatible ASR requests also include a default prompt asking for Simplified Chinese; override it with `AGENSENSE_OPENAI_ASR_PROMPT` or set `AGENSENSE_OPENAI_ASR_LANGUAGE` for providers that honor a language hint.

## Direct LLM

`POST /v1/llm/chat`

```json
{
  "provider_profile_id": "default",
  "client_id": "agendash-desktop",
  "device_label": "MacOS",
  "session_id": "voice-001",
  "messages": [
    {"role": "system", "content": "You are concise."},
    {"role": "user", "content": "hello"}
  ],
  "voice_assistant": {
    "contract": "universal_voice_layer_v1",
    "ui_context": {
      "current_scene": "chat"
    }
  }
}
```

Response:

```json
{
  "provider_profile_id": "default",
  "text": "...",
  "deltas": ["...", "..."]
}
```

`voice_assistant`, `ui_context`, `assistant_intent`, and `metadata` are optional. AgenSense records them for traceability and protocol alignment, but it does not implicitly rewrite `messages`.

For live text output, use `POST /v1/llm/chat/stream` with the same request body. It returns Server-Sent Events:

```text
event: delta
data: {"text":"partial text"}

event: done
data: {"provider_profile_id":"default","text":"full text","deltas":["partial text"]}
```

Streaming text and tool-use metadata can be used together. AgenSense currently streams model text while preserving Universal Voice Layer / MCP metadata in traces; a full tool execution loop should be implemented by the client or a future tool runtime.

## Direct TTS

`POST /v1/tts/synthesize`

```json
{
  "provider_profile_id": "default",
  "client_id": "agendash-desktop",
  "device_label": "MacOS",
  "session_id": "voice-001",
  "text": "hello",
  "format": {
    "codec": "pcm_s16le",
    "sample_rate_hz": 16000,
    "channels": 1
  }
}
```

Response:

```json
{
  "provider_profile_id": "default",
  "format": {
    "codec": "pcm_s16le",
    "sample_rate_hz": 16000,
    "channels": 1
  },
  "audio_base64": "...",
  "chunk_count": 3
}
```

Some OpenAI-compatible TTS services return a WAV container even when PCM is requested. AgenSense unwraps 16-bit PCM WAV responses into `pcm_s16le` frames and reports the actual sample rate and channel count.

Set `AGENSENSE_OPENAI_TTS_VOICE=Serena` for the recommended LocalAI `faster-qwen3-tts` validation path. Voice support is provider-specific; set `AGENSENSE_OPENAI_TTS_VOICE=none` if your backend rejects the voice field. `AGENSENSE_OPENAI_TTS_SENTENCE_STREAM=1` enables sentence-level chunking for long responses, but the default keeps a single provider request for maximum TTS quality.

## Provider Selection

For each direct inference request, AgenSense resolves the provider in this order:

1. explicit `provider_profile_id`
2. namespace default profile
3. the only available profile in the namespace

If no profile can be selected, the request fails.
