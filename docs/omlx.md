# oMLX Setup

AgenSense defaults to a local oMLX OpenAI-compatible service for the GUI Lite voice demo.

Default host runtime address:

```text
http://127.0.0.1:8000/v1
```

When AgenSense runs inside Docker Compose, the same host-side oMLX service is reached as:

```text
http://host.docker.internal:8000/v1
```

## Run oMLX Separately

Start oMLX on the Mac:

```sh
omlx serve
```

Then run AgenSense from source:

```sh
AGENSENSE_OPENAI_TTS_VOICE=none ./scripts/run-local.sh
```

The GUI Lite default connection is still AgenSense itself:

```text
http://127.0.0.1:8080
```

## Docker Compose With Host oMLX

Run AgenSense in Compose while keeping oMLX on the Mac host:

```sh
./scripts/omlx-up.sh
```

Equivalent command:

```sh
docker compose -f compose.yaml -f compose.omlx.yaml up --build
```

This uses:

- `compose.yaml` for AgenSense
- `compose.omlx.yaml` for the oMLX provider environment

## Models

Default AgenSense oMLX model names:

- ASR: `nemotron-3.5-asr-streaming-0.6b-8bit`
- LLM: `gemma-4-E4B-it-MLX-4bit`
- Multimodal: `Qwen3.6-27B-MLX-4bit`
- TTS: `Qwen3-TTS-12Hz-0.6B-Base-8bit`
- VAD: `silero-vad-v6`

Current oMLX builds serve the ASR and TTS models through OpenAI-compatible audio endpoints. `silero-vad-v6` can be recorded in the profile, while the WebSocket voice path uses AgenSense's built-in level-based VAD before ASR.

For voice latency, the default local setup uses the smaller non-thinking Gemma E4B text model, sets `AGENSENSE_OPENAI_REASONING_EFFORT=none`, and enables short-segment TTS with `AGENSENSE_OPENAI_TTS_SENTENCE_STREAM=1`. oMLX's OpenAI-style speech endpoint currently behaves as buffered per utterance, so AgenSense keeps realtime voice segments small instead of depending on a single long TTS request to stream progressively.

## Local CPU TTS Server

For lower-latency Chinese TTS on macOS, run the local CPU speech server:

```sh
./scripts/run-tts-say.sh
```

It exposes an OpenAI-compatible endpoint at:

```text
http://127.0.0.1:18082/v1/audio/speech
```

Register a mixed profile so ASR/LLM stay on oMLX while TTS goes through the CPU server:

```sh
curl -sS \
  -X PUT http://127.0.0.1:8080/v1/providers \
  -H "Authorization: Bearer demo-user-key" \
  -H "content-type: application/json" \
  -d '{
    "id":"omlx-local-macos-tts",
    "name":"oMLX ASR/LLM + Local CPU TTS Server",
    "asr_base_url":"http://127.0.0.1:8000/v1",
    "asr_model":"nemotron-3.5-asr-streaming-0.6b-8bit",
    "llm_base_url":"http://127.0.0.1:8000/v1",
    "llm_model":"gemma-4-E4B-it-MLX-4bit",
    "multimodal_base_url":"http://127.0.0.1:8000/v1",
    "multimodal_model":"Qwen3.6-27B-MLX-4bit",
    "tts_base_url":"http://127.0.0.1:18082/v1",
    "tts_model":"Tingting",
    "vad_base_url":"http://127.0.0.1:8000/v1",
    "vad_model":"silero-vad-v6",
    "default":true
  }'
```

Set `AGENSENSE_OPENAI_TTS_STREAM=1` and `AGENSENSE_OPENAI_TTS_STREAM_BASE_URLS=http://127.0.0.1:18082/v1` when starting AgenSense to include `"stream": true` only for the CPU TTS server. Keep oMLX out of that allowlist because its current streaming speech endpoint rejects `stream=true` with `response_format=pcm`. AgenSense still forwards the resulting audio to GUI Lite with the existing `tts.start` + binary frames + `tts.stop` WebSocket protocol.

Override models when needed:

```sh
export AGENSENSE_DEFAULT_ASR_MODEL="your-asr-model"
export AGENSENSE_DEFAULT_LLM_MODEL="your-llm-model"
export AGENSENSE_DEFAULT_MULTIMODAL_MODEL="your-vision-capable-model"
export AGENSENSE_DEFAULT_TTS_MODEL="your-tts-model"
export AGENSENSE_DEFAULT_VAD_MODEL="your-vad-model"
export AGENSENSE_OPENAI_REASONING_EFFORT="none"
export AGENSENSE_OPENAI_TTS_STREAM="1"
export AGENSENSE_OPENAI_TTS_STREAM_BASE_URLS="http://127.0.0.1:18082/v1"
export AGENSENSE_OPENAI_TTS_SENTENCE_STREAM="1"
export AGENSENSE_OPENAI_TTS_SEGMENT_MAX_RUNES="32"
export AGENSENSE_OPENAI_TTS_SEGMENT_SILENCE_MS="80"
export AGENSENSE_REALTIME_TTS_MAX_RUNES="28"
export AGENSENSE_REALTIME_TTS_SOFT_MIN_RUNES="20"
```

## API Key

Local oMLX normally runs without an API key. If your oMLX service is protected, pass the key to AgenSense:

```sh
export OMLX_API_KEY="replace-me"
export AGENSENSE_DEFAULT_PROVIDER_API_KEY="${OMLX_API_KEY}"
```

With Compose:

```sh
OMLX_API_KEY="replace-me" ./scripts/omlx-up.sh
```

## Quick Checks

oMLX model status:

```sh
curl -sS http://127.0.0.1:8000/v1/models/status
```

AgenSense health:

```sh
curl -sS http://127.0.0.1:8080/healthz
```

Current AgenSense provider profiles:

```sh
curl -sS \
  -H "Authorization: Bearer demo-user-key" \
  http://127.0.0.1:8080/v1/providers
```

Direct LLM through AgenSense:

```sh
curl -sS \
  -X POST http://127.0.0.1:8080/v1/llm/chat \
  -H "Authorization: Bearer demo-user-key" \
  -H 'content-type: application/json' \
  -d '{
    "messages":[
      {"role":"system","content":"You are concise."},
      {"role":"user","content":"hello"}
    ]
  }'
```

Direct TTS through AgenSense:

```sh
curl -sS \
  -X POST http://127.0.0.1:8080/v1/tts/synthesize \
  -H "Authorization: Bearer demo-user-key" \
  -H 'content-type: application/json' \
  -d '{
    "text":"hello from omlx",
    "format":{"codec":"pcm_s16le","sample_rate_hz":16000,"channels":1}
  }'
```
