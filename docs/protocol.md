# Realtime Protocol

AgenSense uses JSON control messages and binary audio frames for the device compatibility WebSocket.

## Transport

Endpoint:

`GET /v1/session/ws`

Required handshake headers:

- `Authorization: Bearer <device_token>`
- `X-Device-Id: <device_id>`
- `X-Protocol-Version: v1`

## Envelope

Control messages use a common envelope:

```json
{
  "type": "hello",
  "request_id": "req-001",
  "payload": {}
}
```

Rules:

- `type` is required
- `payload` is type-specific
- `request_id` is optional but recommended for client-originated control messages
- binary frames are reserved for audio payloads inside an active stream

## Hello

Client:

```json
{
  "type": "hello",
  "request_id": "req-001",
  "payload": {
    "device": {
      "device_id": "vdk-coreS3-001",
      "hardware_sku": "m5cores3-facekit-audio",
      "firmware_version": "1.2.0",
      "capabilities": {
        "display": "lcd",
        "touch": true,
        "usb_hid": true,
        "usb_mic": true
      }
    },
    "state": {
      "config_version": 1
    }
  }
}
```

Server:

```json
{
  "type": "hello.ok",
  "payload": {
    "config_version": 1
  }
}
```

## Audio Stream

Start:

```json
{
  "type": "audio.start",
  "payload": {
    "stream_id": "st-001",
    "codec": "pcm_s16le",
    "sample_rate_hz": 16000,
    "channels": 1
  }
}
```

Then send binary audio frames.

Stop:

```json
{
  "type": "audio.stop",
  "payload": {
    "stream_id": "st-001",
    "last_seq": 1
  }
}
```

The server validates that a binary audio frame belongs to the active stream. The current stream tracker enforces one active stream at a time.

## Server Events

The current mock-friendly device path can emit:

- `asr.final`
- `llm.delta`
- `llm.done`
- `tts.start`
- binary TTS audio frames
- `tts.stop`
- `action.execute`

The voice WebSocket path has its own AgenDash-style event set and is covered by `cmd/agensense-smoke`.

## Actions

`action.execute` is used for UI, HID, or local device actions.

Clients should treat unknown action types as ignorable unless a future capability negotiation explicitly requires support.

## Compatibility Rules

- New JSON fields must be optional.
- Unknown control-message fields should be ignored.
- Unknown event types should be logged and ignored by clients.
- Config snapshots and patches must carry explicit versions.
- Devices should reconnect and send `hello` again after network interruptions.
