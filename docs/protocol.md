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
- `mcp.call.proposed`
- `tts.start`
- binary TTS audio frames
- `tts.stop`
- `action.execute`

The voice WebSocket path has its own AgenDash-style event set and is covered by `cmd/agensense-smoke`.

## Actions

`action.execute` is used for UI, HID, or local device actions.

Clients should treat unknown action types as ignorable unless a future capability negotiation explicitly requires support.

## MCP Call Proposals

`mcp.call.proposed` is emitted by the voice WebSocket path when the client
declares available MCP tools in `voice_assistant.metadata.mcp_tools`,
`voice_assistant.metadata.available_mcp_tools`, `voice_assistant.ui_context.mcp_tools`,
or `voice_assistant.ui_context.available_mcp_tools`.

The event proposes a tool call but does not execute it. Clients or a trusted
gateway decide whether to confirm, execute, rewrite, or ignore the call.

Example `session.update` excerpt:

```json
{
  "type": "session.update",
  "payload": {
    "voice_assistant": {
      "contract": "joyce_voice_capture_v1",
      "ui_context": {
        "available_mcp_tools": [
          "joyce.capture_text",
          "joyce.create_reminder_candidate"
        ]
      }
    }
  }
}
```

Example server event:

```json
{
  "type": "mcp.call.proposed",
  "session_id": "voice-session-001",
  "payload": {
    "proposal_id": "mcp-000001",
    "tool_name": "joyce.create_reminder_candidate",
    "arguments": {
      "raw_text": "今天下午四点提醒我接孩子",
      "title": "接孩子"
    },
    "transcript": "今天下午四点提醒我接孩子",
    "confidence": 0.82,
    "requires_confirmation": true,
    "reason": "The transcript contains a reminder request."
  }
}
```

## Compatibility Rules

- New JSON fields must be optional.
- Unknown control-message fields should be ignored.
- Unknown event types should be logged and ignored by clients.
- Config snapshots and patches must carry explicit versions.
- Devices should reconnect and send `hello` again after network interruptions.
