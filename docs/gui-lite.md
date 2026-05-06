# AgenSense GUI Lite

[AgenSense GUI Lite](https://github.com/agendash/agensense-gui-lite) is a lightweight Flutter validation client for AgenSense.

Use it when you want to verify provider profiles, microphone capture, direct ASR/LLM/TTS APIs, realtime voice WebSocket behavior, TTS playback, device-compatible endpoints, and debug traces without starting the full AgenDash client.

## Recommended Use

Start AgenSense first:

```sh
AGENSENSE_DEBUG=true go run ./cmd/agensense
```

Then run the GUI client:

```sh
cd ../agensense-gui-lite
flutter run -d macos
```

Default connection values:

- Server URL: `http://127.0.0.1:8080`
- API key: `demo-user-key`
- Provider profile: `default`

## What To Test

- Providers tab: list and register LocalAI/OpenAI-compatible provider profiles
- LLM + Tool tab: stream LLM responses and attach Universal Voice Layer / MCP-style metadata
- ASR tab: test direct ASR and streaming ASR mode
- TTS tab: synthesize and replay TTS audio
- Voice WS tab: run microphone -> VAD -> ASR -> LLM -> TTS playback loops
- Device tab: check bootstrap, config, telemetry, and session WebSocket compatibility
- Debug tab: inspect `/debug/api/traces`

## TTS Voice

For TTS backends that support OpenAI-style voices, set:

```sh
AGENSENSE_OPENAI_TTS_VOICE=Serena
```

For LocalAI backends that reject `voice`, set:

```sh
AGENSENSE_OPENAI_TTS_VOICE=none
```

`faster-qwen3-tts` on the current LocalAI test stack accepts named voices such as `Serena`, but backend support is provider-specific.

## Mobile Devices

When running the Flutter client on another device, do not use `127.0.0.1` unless AgenSense is running on that same device. Start AgenSense on a reachable interface:

```sh
AGENSENSE_ADDR=:8080 AGENSENSE_DEBUG=true go run ./cmd/agensense
```

Then point the GUI at the host LAN address, for example:

```text
http://192.168.1.20:8080
```
