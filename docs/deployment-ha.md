# High Availability Notes

This document describes the future HA direction for AgenSense. The current codebase is still a local-first single-process service.

## Recommended Topology

### Control Plane

- HTTP API nodes
- provider registry
- config management
- audit and metrics

### Gateway Plane

- WebSocket gateway nodes
- voice session entrypoints
- device compatibility sessions

### Worker Plane

- provider orchestration
- ASR, LLM, TTS, and future VAD calls
- retry and fallback handling

### Shared Infrastructure

Future production deployments should add:

- PostgreSQL or another durable database for device and provider state
- Redis or KeyDB for presence, rate limits, and short-lived session metadata
- structured logging and metrics collection

## Gateway State

WebSocket connections live on one gateway node, but the gateway should avoid storing critical state only in memory.

Shared state should include:

- device presence
- active config version
- idempotency keys
- rate-limit counters
- reconnect hints

## Reconnect Model

Devices and clients should be able to reconnect cleanly:

- open a new WebSocket
- send `hello`
- fetch or receive the latest config snapshot
- start a new audio stream

Session migration is not a first-stage requirement.

## Provider Resilience

ASR, LLM, and TTS providers are the most volatile dependencies.

Production provider orchestration should include:

- request timeouts
- retries with bounded backoff
- circuit breakers
- provider health checks
- fallback profiles
- per-provider latency and error metrics

## Edge Relay Mode

Edge relay mode is a lightweight deployment shape for constrained sites.

Recommended constraints:

- no local model inference
- no durable primary state
- no heavy metrics stack
- optional read-through config cache
- upstream provider calls remain remote

The edge relay should be treated as a reconnectable gateway, not the source of truth.

## Operational Signals

Minimum useful metrics:

- online device count
- active voice session count
- bootstrap success rate
- provider success rate
- provider p50 and p95 latency
- config ACK latency
- reconnect rate

Useful log fields:

- `tenant_id`
- `instance_id`
- `device_id`
- `session_id`
- `provider_profile_id`
- `request_id`

## Upgrade Strategy

- Upgrade control-plane nodes first.
- Upgrade workers next.
- Upgrade gateways last.
- New protocol fields must be optional and ignorable.
- Config changes must carry explicit versions.

## Message Bus Guidance

Do not introduce Kafka, NATS, or another broker before the gateway/provider boundaries prove they need it.

For the first production-shaped step, Redis is enough for:

- presence
- rate limits
- config version cache
- short-lived coordination
