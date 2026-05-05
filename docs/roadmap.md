# Roadmap

## Milestone 0: Documentation And Contracts

- Keep API and protocol docs aligned with the implementation.
- Preserve Chinese docs under `docs/zh-CN` as the i18n source.
- Keep English docs as the default public documentation.

## Milestone 1: Shared Service Mode

- API-key namespace isolation
- provider profile registration and default selection
- direct ASR, LLM, and TTS APIs
- local JSON persistence

## Milestone 2: Device Compatibility

- bootstrap
- device config and telemetry
- `hello`
- `telemetry.update`
- `audio.start`, binary audio, and `audio.stop`
- `config.snapshot`
- `action.execute`

## Milestone 3: Provider Runtime

- VAD runtime client
- provider health checks
- retry, timeout, and circuit breaker policy
- fallback profile orchestration

## Milestone 4: Production Readiness

- encrypted credential storage
- audit logs
- metrics
- quota and rate limiting
- durable shared store option

## Milestone 5: Client And Device Integrations

- AgenDash
- AgenLeash
- third-party service clients
- hardware devices through the compatibility path

## Milestone 6: Edge Deployment

- ARM64 builds
- lightweight relay mode
- config cache
- controlled degradation when upstream providers are unavailable
