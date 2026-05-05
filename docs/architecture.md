# Architecture

AgenSense is a local-first, modular Go service. The current implementation is intentionally a single process: it keeps the runtime easy to run, easy to test, and easy to split later if the boundaries become stable.

## Positioning

AgenSense is a shared AI sensing gateway, not a hardware-specific server.

Primary callers:

- AgenDash and other GUI clients
- AgenLeash and other backend components
- third-party services that need reusable ASR, LLM, and TTS access

The device bootstrap and WebSocket path remains available for hardware compatibility, but it is no longer the only product shape.

## Design Principles

### Shared Service First

Any client with an `AGENSENSE_API_KEY` should be able to:

- register provider profiles
- persist provider configuration under a stable namespace
- call ASR, LLM, and TTS APIs directly

### Thin Clients

Clients should own capture, playback, UI, and local interaction. AgenSense owns provider configuration, provider selection, model API adaptation, and device protocol compatibility.

### Unified Provider Abstraction

Provider-specific APIs are normalized behind a small capability model:

- speech-to-text
- chat
- text-to-speech
- future VAD runtime

This lets GUI clients, services, and devices avoid coupling directly to each model vendor.

### Modular Monolith First

The first production-shaped step is still one binary:

- HTTP API
- provider registry
- direct inference
- device compatibility gateway
- local file-backed repository

The code is split internally so these boundaries can later become separate processes if needed.

## Runtime Layers

### Provider Registry

Responsibilities:

- read `Authorization: Bearer <AGENSENSE_API_KEY>`
- map each API key to a stable namespace
- persist provider profiles under that namespace
- store the namespace default provider profile

The isolation boundary is the API key namespace, not a hardware `device_id`.

### Direct Inference

Responsibilities:

- serve `POST /v1/asr/transcribe`
- serve `POST /v1/llm/chat`
- serve `POST /v1/tts/synthesize`
- resolve the requested provider profile
- call `mock://` or an OpenAI-compatible upstream

This is the recommended path for AgenDash, AgenLeash, and ordinary service callers.

### Voice WebSocket

Responsibilities:

- accept AgenDash-style realtime voice sessions
- receive audio chunks
- run VAD-like turn detection in the current mock/local runtime
- emit ASR, LLM, TTS, and response lifecycle events
- optionally capture debug traces and audio assets

### Device Compatibility Gateway

Responsibilities:

- handle bootstrap and device authentication
- serve device config and telemetry endpoints
- maintain the legacy device WebSocket session
- support `hello`, `audio.start`, binary audio frames, and `audio.stop`

This path is kept for hardware compatibility and protocol regression.

### Provider Adapter

Implemented:

- `mock://`
- OpenAI-compatible ASR
- OpenAI-compatible LLM
- OpenAI-compatible TTS

Not implemented yet:

- dedicated VAD runtime client
- provider health checks
- circuit breaker and fallback orchestration
- long-lived provider WebSocket or gRPC pools

### State

The current runtime uses a single local JSON store.

Persisted data includes:

- provider profiles
- namespace default provider snapshots
- device records
- issued device-token hashes
- device config snapshots

This is appropriate for local development and trusted single-node deployments. Multi-node production requires a durable shared store.

## Core Entities

### API Key Namespace

The main isolation unit for shared-service mode.

- derived from `AGENSENSE_API_KEY`
- original API key is not stored
- owns provider profiles and the default profile

### Provider Profile

A provider profile describes one upstream capability bundle:

- `asr_base_url`, `asr_api_key`, `asr_model`
- `llm_base_url`, `llm_api_key`, `llm_model`
- `tts_base_url`, `tts_api_key`, `tts_model`
- future `vad_*` fields

Provider API keys are currently stored as plain text in the local JSON store.

### Device

Devices only matter on the hardware compatibility path.

Typical fields:

- `device_id`
- `tenant_id`
- `instance_id`
- `hardware_sku`
- `chip_id`
- `capabilities`
- config versions

Ordinary GUI and service clients do not need device records.

## Current Boundaries

Implemented:

- API-key namespace isolation
- provider profile registry
- direct ASR, LLM, and TTS APIs
- mock and OpenAI-compatible provider clients
- device bootstrap and WebSocket compatibility path
- voice WebSocket smoke coverage
- optional debug trace UI

Still pending:

- encrypted provider credential storage
- provider health checks
- quotas and rate limiting
- audit logs and metrics
- multi-node shared store
- unified provider orchestration for the legacy device WebSocket path
