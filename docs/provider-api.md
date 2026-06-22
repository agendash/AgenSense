# Provider API

The provider API is the primary shared-service interface for AgenSense. It lets clients register reusable upstream model profiles and then call ASR, LLM, multimodal vision, and TTS without embedding provider-specific code in every client.

## Authentication

Provider registry and direct inference endpoints use:

```http
Authorization: Bearer <AGENSENSE_API_KEY>
```

The API key maps to a stable internal namespace. The raw API key is not stored.

By default, AgenSense seeds the `demo-user-key` namespace with a local oMLX default provider:

- base URL: `http://127.0.0.1:8000/v1`
- ASR model: `nemotron-3.5-asr-streaming-0.6b-8bit`
- LLM model: `gemma-4-E4B-it-MLX-4bit`
- Multimodal model: `Qwen3.6-27B-MLX-4bit`
- TTS model: `Qwen3-TTS-12Hz-0.6B-Base-8bit`
- VAD model: `silero-vad-v6`

Current oMLX builds serve the ASR and TTS models through OpenAI-compatible audio endpoints. `silero-vad-v6` can be recorded in the profile, while the WebSocket voice path uses AgenSense's built-in level-based VAD before ASR.

AgenSense can also target a local CPU TTS server for the TTS portion only. `cmd/agensense-tts-say` exposes `POST /v1/audio/speech` and can be registered as a profile's `tts_base_url` while ASR and LLM remain on oMLX.

## Provider Profiles

Supported fields:

- `asr_base_url`
- `asr_api_key`
- `asr_model`
- `llm_base_url`
- `llm_api_key`
- `llm_model`
- `multimodal_base_url`
- `multimodal_api_key`
- `multimodal_model`
- `tts_base_url`
- `tts_api_key`
- `tts_model`
- `vad_base_url`
- `vad_api_key`
- `vad_model`

Provider credentials are currently persisted as plain text in the local JSON store. Treat the first implementation as local-dev or trusted single-node infrastructure.

## Register A Provider

`POST /v1/providers`

```json
{
  "id": "omlx-local",
  "name": "oMLX Local Voice Stack",
  "asr_base_url": "http://127.0.0.1:8000/v1",
  "asr_api_key": "******",
  "asr_model": "nemotron-3.5-asr-streaming-0.6b-8bit",
  "llm_base_url": "http://127.0.0.1:8000/v1",
  "llm_api_key": "******",
  "llm_model": "gemma-4-E4B-it-MLX-4bit",
  "multimodal_base_url": "http://127.0.0.1:8000/v1",
  "multimodal_api_key": "******",
  "multimodal_model": "Qwen3.6-27B-MLX-4bit",
  "tts_base_url": "http://127.0.0.1:8000/v1",
  "tts_api_key": "******",
  "tts_model": "Qwen3-TTS-12Hz-0.6B-Base-8bit",
  "vad_base_url": "http://127.0.0.1:8000/v1",
  "vad_model": "silero-vad-v6",
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

## Direct Multimodal / Vision

`POST /v1/multimodal/chat`

```json
{
  "provider_profile_id": "default",
  "client_id": "agendash-desktop",
  "session_id": "vision-001",
  "messages": [
    {
      "role": "user",
      "content": [
        {"type": "text", "text": "What is this?"},
        {
          "type": "image_url",
          "image_url": {
            "url": "data:image/png;base64,iVBORw0KGgo..."
          }
        }
      ]
    }
  ]
}
```

Response:

```json
{
  "provider_profile_id": "default",
  "text": "..."
}
```

`POST /v1/vision/analyze` is a convenience wrapper for common image analysis calls:

```json
{
  "provider_profile_id": "default",
  "prompt": "Describe this UI screenshot and list visual problems.",
  "images": [
    {
      "image_base64": "iVBORw0KGgo...",
      "mime_type": "image/png"
    }
  ]
}
```

If `multimodal_*` fields are omitted from a provider profile, AgenSense falls back to the profile's LLM base URL, API key, and model. The default seeded multimodal model also inherits `AGENSENSE_DEFAULT_LLM_MODEL` unless `AGENSENSE_DEFAULT_MULTIMODAL_MODEL` is set.

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

Set `AGENSENSE_OPENAI_REASONING_EFFORT=none` to suppress thinking on compatible OpenAI-style LLM backends. Set `AGENSENSE_OPENAI_TTS_VOICE=Serena` for the recommended LocalAI `faster-qwen3-tts` validation path. Voice support is provider-specific; set `AGENSENSE_OPENAI_TTS_VOICE=none` if your backend rejects the voice field. `AGENSENSE_OPENAI_TTS_SENTENCE_STREAM=1` enables provider-side text chunking for long TTS responses; tune `AGENSENSE_OPENAI_TTS_SEGMENT_MAX_RUNES`, `AGENSENSE_REALTIME_TTS_MAX_RUNES`, and `AGENSENSE_REALTIME_TTS_SOFT_MIN_RUNES` lower when the upstream TTS model buffers each utterance before returning audio.

## Provider Selection

For each direct inference request, AgenSense resolves the provider in this order:

1. explicit `provider_profile_id`
2. namespace default profile
3. the only available profile in the namespace

If no profile can be selected, the request fails.

## Provider Capabilities

For OpenAI-compatible HTTP providers:

- ASR calls `/audio/transcriptions`
- LLM calls `/chat/completions` with streaming enabled
- Multimodal calls `/chat/completions` with OpenAI-style `text` and `image_url` content parts
- TTS calls `/audio/speech`
