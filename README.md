# AgenSense

Reusable AI sensing gateway for AgenDash, AgenLeash, hardware devices, and other clients.

AgenSense does not run LLM, ASR, TTS, multimodal vision, or VAD models on edge devices by default. It provides a stable gateway, provider registry, direct inference API, and device-compatible voice protocol so clients can share the same model configuration and runtime behavior.

Chinese documentation is available in [README.zh-CN.md](README.zh-CN.md).

## Status

AgenSense is currently a local-first Go service with two supported access paths:

- Shared service mode: clients use `Authorization: Bearer <AGENSENSE_API_KEY>` to register provider profiles and call ASR, LLM, multimodal vision, and TTS APIs directly.
- Device compatibility mode: devices use bootstrap, device tokens, and a WebSocket voice session for ESP32/M5Stack/HID-style integrations.

The shared service mode is the preferred path for desktop, GUI, service, and third-party clients. The device path remains useful for hardware integration and protocol regression.

## Features

- Single-binary Go service under `cmd/agensense`
- File-backed local JSON store under `AGENSENSE_DATA_DIR`
- API-key namespace isolation for provider profiles
- Direct APIs:
  - `POST /v1/asr/transcribe`
  - `POST /v1/llm/chat`
  - `POST /v1/multimodal/chat`
  - `POST /v1/vision/analyze`
  - `POST /v1/tts/synthesize`
- Provider support:
  - `mock://` provider for local development
  - OpenAI-compatible ASR, LLM, multimodal, and TTS clients
- Device compatibility APIs:
  - `POST /v1/bootstrap`
  - `GET /v1/device/config`
  - `POST /v1/device/telemetry`
  - `GET /v1/session/ws`
- Voice WebSocket path for AgenDash-style realtime voice sessions:
  - `GET /v1/voice/ws`
- Optional debug trace UI:
  - `GET /debug/traces`

## Quick Start

Install with Homebrew after a tagged release has published the cask:

```sh
brew install --cask agendash/tap/agensense
agensense -version
agensense
```

Or build from source:

```sh
go test ./...
go build ./cmd/agensense
go run ./cmd/agensense
```

The service listens on `127.0.0.1:8080` by default.

```sh
curl -sS http://127.0.0.1:8080/healthz
```

The default provider profile points at the local oMLX OpenAI-compatible service:

- API key: `demo-user-key`
- profile id: `omlx-local`
- base URL: `http://127.0.0.1:8000/v1`
- models: ASR `nemotron-3.5-asr-streaming-0.6b-8bit`, LLM `gemma-4-E4B-it-MLX-4bit`, multimodal `Qwen3.6-27B-MLX-4bit`, TTS `Qwen3-TTS-12Hz-0.6B-Base-8bit`, VAD `silero-vad-v6`

The WebSocket voice path uses AgenSense's built-in level-based VAD before sending speech segments to ASR. The oMLX `silero-vad-v6` model can be stored in provider profiles, but current oMLX builds do not expose it through the same served audio API as ASR/TTS.

## Direct Inference

```sh
export AGENSENSE_API_KEY="demo-user-key"

curl -sS \
  -X POST http://127.0.0.1:8080/v1/llm/chat \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}" \
  -H 'content-type: application/json' \
  -d '{
    "messages":[
      {"role":"system","content":"You are concise."},
      {"role":"user","content":"hello"}
    ]
  }'
```

To point AgenSense at an OpenAI-compatible upstream, register a provider profile:

```sh
export AGENSENSE_API_KEY="demo-user-key"
export PROVIDER_BASE_URL="http://127.0.0.1:8000/v1"
export PROVIDER_API_KEY=""

curl -sS \
  -X POST http://127.0.0.1:8080/v1/providers \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}" \
  -H 'content-type: application/json' \
  -d '{
    "id":"omlx-local",
    "name":"oMLX Local Voice Stack",
    "asr_base_url":"'"${PROVIDER_BASE_URL}"'",
    "asr_api_key":"'"${PROVIDER_API_KEY}"'",
    "asr_model":"nemotron-3.5-asr-streaming-0.6b-8bit",
    "llm_base_url":"'"${PROVIDER_BASE_URL}"'",
    "llm_api_key":"'"${PROVIDER_API_KEY}"'",
    "llm_model":"gemma-4-E4B-it-MLX-4bit",
    "multimodal_base_url":"'"${PROVIDER_BASE_URL}"'",
    "multimodal_api_key":"'"${PROVIDER_API_KEY}"'",
    "multimodal_model":"Qwen3.6-27B-MLX-4bit",
    "tts_base_url":"'"${PROVIDER_BASE_URL}"'",
    "tts_api_key":"'"${PROVIDER_API_KEY}"'",
    "tts_model":"Qwen3-TTS-12Hz-0.6B-Base-8bit",
    "vad_base_url":"'"${PROVIDER_BASE_URL}"'",
    "vad_api_key":"'"${PROVIDER_API_KEY}"'",
    "vad_model":"silero-vad-v6",
    "default":true
  }'
```

Provider credentials are currently stored in the local JSON store. Use this mode for local development or trusted single-node deployments until encrypted credential storage is added.

## Smoke And Tests

Keep these in Git:

- `cmd/agensense-smoke`: source-controlled end-to-end smoke runner for voice WebSocket, ASR, LLM, TTS, and optional AgenLeash workspace checks.
- `*_test.go`: normal Go unit and integration tests. These are required for safe changes and should stay in the repository.

Generated smoke artifacts are written under `tmp/smoke/...`; `tmp/` is ignored and should not be committed.

Run the smoke runner after the service is up:

```sh
go run ./cmd/agensense-smoke
```

## GUI Lite Validation

For manual validation, use [AgenSense GUI Lite](https://github.com/agendash/agensense-gui-lite). It is a lightweight Flutter client for provider registration, direct ASR/LLM/TTS checks, realtime Voice WS testing, device compatibility checks, and debug trace inspection.

```sh
cd ../agensense-gui-lite
flutter run -d macos
```

See [docs/gui-lite.md](docs/gui-lite.md) for the full validation workflow.

## Deployment

Local scripts:

```sh
./scripts/run-local.sh
./scripts/smoke-local.sh
```

Docker Compose:

```sh
docker compose up --build
```

Docker Compose with host oMLX:

```sh
./scripts/omlx-up.sh
```

Full local stack with LocalAI:

```sh
./scripts/localai-up.sh
```

See [docs/deployment.md](docs/deployment.md) for binary, shell script, and Docker Compose workflows.
See [docs/omlx.md](docs/omlx.md) for oMLX setup and model-name expectations.
See [docs/localai.md](docs/localai.md) for LocalAI setup and model-name expectations.

## Configuration

Common environment variables:

- `AGENSENSE_ADDR`: listen address, default `:8080`
- `AGENSENSE_PUBLIC_BASE_URL`: public base URL used in device bootstrap responses
- `AGENSENSE_DATA_DIR`: state, logs, and debug trace data directory
- `AGENSENSE_LOG_LEVEL`: `debug`, `info`, `warn`, or `error`
- `AGENSENSE_DEBUG=true`: enables `/debug/*` trace UI and audio assets
- `AGENSENSE_DISABLE_DEMO_SEED=true`: disables the built-in demo device seed
- `AGENSENSE_DEFAULT_PROVIDER_BASE_URL`: default provider base URL, default `http://127.0.0.1:8000/v1`
- `AGENSENSE_DEFAULT_PROVIDER_API_KEY`: default upstream provider API key
- `AGENSENSE_DEFAULT_MULTIMODAL_MODEL`: optional default multimodal model; inherits `AGENSENSE_DEFAULT_LLM_MODEL` when unset
- `AGENSENSE_ASR_CHINESE_SCRIPT`: Chinese transcript normalization, default `zh-Hans`; set `original` to keep upstream ASR text unchanged
- `AGENSENSE_OPENAI_ASR_LANGUAGE`: optional OpenAI-compatible ASR language hint
- `AGENSENSE_OPENAI_ASR_PROMPT`: optional OpenAI-compatible ASR prompt; the default asks Chinese transcripts to use Simplified Chinese
- `AGENSENSE_OPENAI_REASONING_EFFORT`: OpenAI-compatible reasoning effort, default local oMLX value `none`
- `AGENSENSE_OPENAI_TTS_VOICE`: OpenAI-compatible TTS voice; use `none` for oMLX, while some LocalAI TTS backends accept named voices such as `Serena`
- `AGENSENSE_OPENAI_TTS_RESPONSE_FORMAT`: requested TTS response format, default `pcm`
- `AGENSENSE_OPENAI_TTS_SENTENCE_STREAM`: optional provider-side TTS text chunking, default local oMLX value `1`
- `AGENSENSE_OPENAI_TTS_SEGMENT_MAX_RUNES`: max text runes per provider TTS request, default local oMLX value `32`
- `AGENSENSE_OPENAI_TTS_SEGMENT_SILENCE_MS`: silence inserted between provider TTS segments, default local oMLX value `80`
- `AGENSENSE_REALTIME_TTS_MAX_RUNES`: max realtime Voice WS text runes before a TTS segment is emitted, default `28`
- `AGENSENSE_REALTIME_TTS_SOFT_MIN_RUNES`: soft punctuation split threshold for realtime Voice WS TTS, default `20`

## Documentation

- [Provider API](docs/provider-api.md)
- [Local runbook](docs/mvp-local-runbook.md)
- [AgenSense GUI Lite](docs/gui-lite.md)
- [Architecture](docs/architecture.md)
- [Device bootstrap](docs/device-bootstrap.md)
- [Realtime protocol](docs/protocol.md)
- [Deployment](docs/deployment.md)
- [oMLX setup](docs/omlx.md)
- [LocalAI setup](docs/localai.md)
- [Release process](docs/release.md)
- [Client integration skill](skills/agensense-client/SKILL.md)
- [HA deployment notes](docs/deployment-ha.md)
- [Roadmap](docs/roadmap.md)
- [Developer handoff](docs/dev-handoff.md)

## Repository Layout

- `cmd/agensense`: service entrypoint
- `cmd/agensense-smoke`: local voice smoke runner
- `internal/httpapi`: HTTP routes and auth entrypoints
- `internal/voicews`: AgenDash-style voice WebSocket session
- `internal/gateway`: device compatibility WebSocket session
- `internal/protocol`: JSON envelope, event types, and stream rules
- `internal/provider`: mock and OpenAI-compatible provider clients
- `internal/service`: provider registry, inference orchestration, and device control
- `internal/store`: file-backed local repository
- `deploy`: deployment-adjacent notes and examples
- `scripts`: local run and smoke helper scripts
- `skills`: public client-integration skills for agents building on AgenSense
