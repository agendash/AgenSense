# Local Runbook

This runbook verifies the current local AgenSense runtime. It is not a production deployment guide.

## What To Verify

Shared service mode:

- service starts
- `AGENSENSE_API_KEY` selects a stable namespace
- provider profiles can be registered and queried
- ASR, LLM, and TTS direct APIs work

Device compatibility mode:

- a device can call `POST /v1/bootstrap`
- a device can open `GET /v1/session/ws`
- the server emits the expected mock-friendly event stream

Voice WebSocket mode:

- `cmd/agensense-smoke` can exercise the AgenDash-style voice path
- optional debug traces are captured when `AGENSENSE_DEBUG=true`

## Start Locally

```sh
go test ./...
go build ./cmd/agensense
go run ./cmd/agensense
```

Or use the helper script:

```sh
./scripts/run-local.sh
```

Default runtime:

- listen address: `127.0.0.1:8080`
- data directory: `tmp/agensense`
- default API key: `demo-user-key`
- default provider profile: `omlx-local`
- default provider base URL: `http://127.0.0.1:8000/v1`
- default models: `nemotron-3.5-asr-streaming-0.6b-8bit`, `gemma-4-E4B-it-MLX-4bit`, `Qwen3-TTS-12Hz-0.6B-Base-8bit`

## Health Check

```sh
curl -sS http://127.0.0.1:8080/healthz
```

Expected:

```json
{"ok":true}
```

## Shared Service Flow

Prepare an API key:

```sh
export AGENSENSE_API_KEY="demo-user-key"
```

List provider profiles:

```sh
curl -sS \
  http://127.0.0.1:8080/v1/providers \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}"
```

The default profile expects oMLX on host port `8000`. If you use a different OpenAI-compatible provider, register or override the profile:

```sh
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
    "tts_base_url":"'"${PROVIDER_BASE_URL}"'",
    "tts_api_key":"'"${PROVIDER_API_KEY}"'",
    "tts_model":"Qwen3-TTS-12Hz-0.6B-Base-8bit",
    "vad_base_url":"'"${PROVIDER_BASE_URL}"'",
    "vad_api_key":"'"${PROVIDER_API_KEY}"'",
    "vad_model":"silero-vad-v6",
    "default":true
  }'
```

## Direct LLM Check

```sh
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

Expected fields:

- `provider_profile_id`
- `text`
- `deltas`

If oMLX is not running yet, start with [oMLX setup](omlx.md) or temporarily register a mock provider:

```sh
curl -sS \
  -X POST http://127.0.0.1:8080/v1/providers \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}" \
  -H 'content-type: application/json' \
  -d '{
    "id":"default",
    "name":"Mock Default",
    "asr_base_url":"mock://asr",
    "asr_model":"mock-asr",
    "llm_base_url":"mock://llm",
    "llm_model":"mock-llm",
    "tts_base_url":"mock://tts",
    "tts_model":"mock-tts",
    "default":true
  }'
```

## Direct TTS Check

```sh
curl -sS \
  -X POST http://127.0.0.1:8080/v1/tts/synthesize \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}" \
  -H 'content-type: application/json' \
  -d '{
    "text":"hello",
    "format":{
      "codec":"pcm_s16le",
      "sample_rate_hz":16000,
      "channels":1
    }
  }'
```

Expected fields:

- `provider_profile_id`
- `format`
- `audio_base64`
- `chunk_count`

## Direct ASR Check

```sh
curl -sS \
  -X POST http://127.0.0.1:8080/v1/asr/transcribe \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}" \
  -H 'content-type: application/json' \
  -d '{
    "format":{
      "codec":"pcm_s16le",
      "sample_rate_hz":16000,
      "channels":1
    },
    "audio_base64":"AQIDBAU="
  }'
```

Expected fields:

- `provider_profile_id`
- `text`

## Voice Smoke

Start the service first, then run:

```sh
go run ./cmd/agensense-smoke
```

Or:

```sh
./scripts/smoke-local.sh
```

Default behavior:

- target service: `http://127.0.0.1:8080`
- API key: `demo-user-key`
- provider profile: `smoke-mock`
- input audio source: TTS seed audio
- output artifacts: `tmp/smoke/<session-id>`

The smoke runner verifies:

- `session.ready`
- `vad.state`
- `asr.final`
- `llm.delta`
- `llm.done`
- `tts.start`
- downstream binary TTS audio
- `tts.stop`

## GUI Lite Validation

For interactive checks, use [AgenSense GUI Lite](https://github.com/agendash/agensense-gui-lite):

```sh
cd ../agensense-gui-lite
flutter run -d macos
```

Use the GUI to validate provider registration, direct ASR/LLM/TTS APIs, streaming ASR, realtime Voice WS, device compatibility endpoints, and `/debug/api/traces`.

See [AgenSense GUI Lite](gui-lite.md) for details.
- `response.done`
- optional debug trace and audio assets

Fast protocol-only mode:

```sh
go run ./cmd/agensense-smoke \
  -input-source=tone \
  -realtime=false
```

Use the current default provider instead of the mock smoke profile:

```sh
go run ./cmd/agensense-smoke \
  -ensure-mock-provider=false \
  -provider-profile-id=omlx-local \
  -timeout=90s
```

## Optional AgenLeash Workspace Check

```sh
go run ./cmd/agensense-smoke \
  -realtime=false \
  -agenleash-base-url=http://127.0.0.1:8081 \
  -agenleash-token=<AGENLEASH_TOKEN> \
  -agenleash-workspace="$(pwd)" \
  -agenleash-adapter=codex \
  -agenleash-wait=45s \
  -agenleash-message='Reply with exactly: AgenSense workspace smoke ok'
```

## Device Compatibility Check

Default demo device:

- `device_id`: `vdk-coreS3-001`
- `chip_id`: `esp32s3-abcdef`
- `hardware_sku`: `m5cores3-facekit-audio`
- `claim_token`: `factory-claim-token`
- `provider_profile_id`: `default`

Bootstrap request:

```json
{
  "device_id": "vdk-coreS3-001",
  "chip_id": "esp32s3-abcdef",
  "hardware_sku": "m5cores3-facekit-audio",
  "firmware_version": "1.2.0",
  "firmware_channel": "stable",
  "capabilities": {
    "display": "lcd",
    "touch": true,
    "usb_hid": true,
    "usb_mic": true,
    "cellular": false
  },
  "claim_token": "factory-claim-token"
}
```

Expected bootstrap response fields:

- `device_token`
- `ws_url`
- `config_version`
- `config`
- `device_id`

Open the device WebSocket with:

- `Authorization: Bearer <device_token>`
- `X-Device-Id: <device_id>`
- `X-Protocol-Version: v1`

## Evidence To Keep

- `go test ./...`
- `go build ./cmd/agensense`
- one successful `go run ./cmd/agensense-smoke`
- one provider registration and query result
- one direct LLM, TTS, and ASR response
- optional bootstrap response and WebSocket event order

Generated artifacts under `tmp/` should not be committed.
