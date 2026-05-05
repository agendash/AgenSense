# Related Open Source Options

This document records projects and patterns worth reviewing. AgenSense does not currently vendor or depend on these projects.

## Xiaozhi ESP32 Server

Repository:

- <https://github.com/xinnan-tech/xiaozhi-esp32-server>

Useful ideas:

- embedded-device voice flow
- ESP32-oriented protocol decisions
- practical device-server integration patterns

Why AgenSense is separate:

- AgenSense also targets GUI and service clients.
- Provider profiles are first-class shared-service state.
- Device compatibility is one path, not the whole product boundary.

## OpenAI-Compatible Provider APIs

Useful because many local and hosted model runtimes expose OpenAI-compatible endpoints.

AgenSense currently uses this shape for:

- ASR
- LLM chat
- TTS

The compatibility layer should remain narrow and testable.

## WebRTC Media Servers

Media servers are useful references for realtime transport and observability, but they are not a direct fit for the current device WebSocket path.

AgenSense currently keeps the protocol simpler:

- JSON control messages
- binary audio frames
- local mock regression
- provider calls over HTTP

## What AgenSense Owns

AgenSense should keep control over:

- provider registry
- API-key namespace isolation
- device bootstrap
- realtime protocol envelope
- debug trace format
- direct inference API

These are the core product contracts that should not be delegated too early.
