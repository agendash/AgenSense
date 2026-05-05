# Developer Handoff

This checklist captures the current AgenSense implementation and the next useful engineering steps.

## Current State

Implemented:

- single-process service entrypoint in `cmd/agensense`
- file-backed repository in `internal/store`
- API-key provider registry
- direct ASR, LLM, and TTS APIs
- mock and OpenAI-compatible provider clients
- device bootstrap and compatibility WebSocket path
- AgenDash-style voice WebSocket path
- debug trace store and debug trace UI
- local smoke runner in `cmd/agensense-smoke`
- Dockerfile and Docker Compose local deployment example
- LocalAI default provider configuration

## Keep In Git

Keep:

- `cmd/agensense-smoke`: source code for end-to-end regression, not a generated artifact
- `*_test.go`: Go test files required for CI and safe refactors
- `Dockerfile`, `compose.yaml`, and `scripts/*.sh`: reproducible local deployment workflow

Do not commit:

- `tmp/`
- built binaries
- logs
- local JSON state
- provider keys or `.env` files

## Validation

Before publishing changes:

```sh
go test ./...
go build ./cmd/agensense
go run ./cmd/agensense-smoke
```

The smoke runner requires the service to be running.

## Documentation Rules

- Default docs are English.
- Chinese docs are preserved under `docs/zh-CN`.
- Use `AgenSense` in prose.
- Keep binary paths, package names, env vars, and commands lowercase where required.
- Update `docs/SUMMARY.md` when adding public docs.
- Update `docs/zh-CN/SUMMARY.md` when adding Chinese i18n docs.

## Next Engineering Steps

Recommended order:

1. Add CI for `go test ./...` and `go build ./cmd/agensense`.
2. Add encrypted credential storage or a secret-provider abstraction.
3. Add provider health checks.
4. Add timeout, retry, and fallback policies around provider calls.
5. Add metrics for provider latency and voice session state.
6. Decide whether the file store is enough or a database adapter is needed.
7. Align the legacy device WebSocket path with the provider registry.

## Risk Notes

- Provider credentials are currently stored in local JSON as plain text.
- The local store is not safe for multi-node deployment.
- The legacy device WebSocket path still uses the mock pipeline.
- Docker Compose is a local example, not a production topology.
- The default LocalAI model IDs must exist in the target LocalAI instance or be overridden with env vars.
