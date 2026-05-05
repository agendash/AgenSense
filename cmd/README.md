# Commands

This directory contains executable entrypoints.

## `cmd/agensense`

The main AgenSense service.

```sh
go run ./cmd/agensense
```

## `cmd/agensense-smoke`

End-to-end voice smoke runner. Keep this in Git because it is source code for repeatable local regression, not generated output.

It verifies the AgenDash-style voice WebSocket path:

- session readiness
- VAD state events
- ASR final text
- LLM deltas
- TTS binary audio
- optional debug trace assets

Run it after the service is running:

```sh
go run ./cmd/agensense-smoke
```

Useful variants:

```sh
# Use a generated tone instead of seed TTS audio.
go run ./cmd/agensense-smoke -input-source=tone

# Use the current default provider instead of the mock smoke profile.
go run ./cmd/agensense-smoke -ensure-mock-provider=false -provider-profile-id=default -timeout=90s

# Also verify an AgenLeash workspace session.
go run ./cmd/agensense-smoke \
  -agenleash-base-url=http://127.0.0.1:8081 \
  -agenleash-token=<AGENLEASH_TOKEN> \
  -agenleash-workspace="$(pwd)"
```
