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
- LLM model: `gemma-4-e2b-it`
- TTS model: `tts-1`

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
  "llm_model": "gpt-4o-mini",
  "tts_base_url": "http://127.0.0.1:8081/v1",
  "tts_api_key": "******",
  "tts_model": "tts-1",
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

Some OpenAI-compatible TTS services return a WAV container even when PCM is requested. AgenSense detects WAV headers and reports `format.codec=wav`.

## Provider Selection

For each direct inference request, AgenSense resolves the provider in this order:

1. explicit `provider_profile_id`
2. namespace default profile
3. the only available profile in the namespace

If no profile can be selected, the request fails.
