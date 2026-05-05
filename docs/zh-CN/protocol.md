# 设备实时协议

## 目标

协议要稳定、简单、低耦合。

设备不应该直接知道某个 ASR / LLM / TTS 厂商的细节。设备只跟 `AgenSense Protocol` 说话。

当前 MVP 的实现基线以 `internal/protocol` 为准。特别是：

- 所有 JSON 控制消息都统一使用 `payload` 包裹
- `hello` 也不能例外
- 早期文档里的扁平 `hello` 示例应视为过时写法

## 传输

- 控制通道：WebSocket
- 控制消息：JSON
- 音频数据：WebSocket binary frame
- 可选：后续支持 WebRTC，但第一阶段不作为设备主协议

## 握手

连接地址：

`wss://gateway.example.com/v1/session/ws`

Header：

- `Authorization: Bearer <device_token>`
- `X-Device-Id: <device_id>`
- `X-Protocol-Version: v1`

连接建立后，设备先发：

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
        "usb_mic": true,
        "cellular": false
      }
    },
    "state": {
      "config_version": 12
    }
  }
}
```

服务端响应：

```json
{
  "type": "hello.ok",
  "request_id": "req-001",
  "session_id": "sess-abc",
  "ts_ms": 1770000000,
  "payload": {
    "server_time_ms": 1770000000,
    "desired_config_version": 12
  }
}
```

## 消息模型

MVP 里所有 JSON 消息都统一包一层：

```json
{
  "type": "event.name",
  "request_id": "optional-client-request-id",
  "session_id": "optional-session-id",
  "ts_ms": 1770000000,
  "payload": {}
}
```

## 关键事件

### AgenDash 语音会话扩展

AgenDash 风格客户端可以先发 `session.update` 来设置会话上下文：

```json
{
  "type": "session.update",
  "payload": {
    "client_id": "agendash-desktop",
    "session_id": "voice-session-001",
    "provider_profile_id": "default",
    "response_language": "auto",
    "voice_assistant": {
      "contract": "universal_voice_layer_v1",
      "ui_context": {
        "current_scene": "chat"
      }
    }
  }
}
```

`response_language` 可选值为 `auto`、`zh-Hans`、`zh-Hant`、`en`。`auto` 会跟随用户输入的大语言方向，但中文回复默认使用简体，避免 ASR 转写成繁体时把 TTS 回复也带成繁体。

### 设备到服务端

#### `telemetry.update`

上报网络、电量、音频电平、硬件状态。

#### `input.trigger`

上报按钮、触摸、编码器等触发。

#### `audio.start`

开始一段上行语音流。

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

当前 MVP 只接受 `pcm_s16le`。

#### binary frame

当前 `stream_id` 的原始 PCM 数据。

MVP 的规则是：

- 每个方向同一时间最多只有一个 open stream
- binary frame 归属于该方向最近一个打开且未关闭的 stream
- `last_seq` 不是“字节偏移”，而是已经接收的 binary frame 数量

如果后面需要多路复用，再加 frame header。

#### `audio.stop`

```json
{
  "type": "audio.stop",
  "payload": {
    "stream_id": "st-001",
    "last_seq": 128
  }
}
```

`last_seq` 必须等于该 `stream_id` 已收到的 binary frame 数，否则应返回错误。

#### `config.ack`

设备确认某个配置版本已经应用。

#### `action.result`

设备回报某个 HID / UI / local action 的执行结果。

### 服务端到设备

#### `config.snapshot`

下发完整配置。

#### `config.patch`

下发增量配置。

#### `asr.partial`

实时识别中间结果。

#### `asr.final`

最终识别文本。

#### `llm.delta`

流式文本增量。

#### `llm.done`

一轮回复结束。

#### `tts.start`

开始一段下行语音。

MVP 里它复用和 `audio.start` 相同的 payload 结构：

```json
{
  "type": "tts.start",
  "session_id": "sess-abc",
  "payload": {
    "stream_id": "tts-001",
    "codec": "pcm_s16le",
    "sample_rate_hz": 16000,
    "channels": 1
  }
}
```

#### binary frame

TTS 音频块。

#### `tts.stop`

下行语音结束。

MVP 里它也复用和 `audio.stop` 相同的 payload 结构，并要求 `last_seq` 等于下行 binary frame 数量。

#### `action.execute`

让设备执行结构化动作。

```json
{
  "type": "action.execute",
  "payload": {
    "action_id": "act-001",
    "kind": "noop",
    "payload": {
      "reason": "mock pipeline complete"
    }
  }
}
```

#### `error`

统一错误消息。

```json
{
  "type": "error",
  "session_id": "sess-abc",
  "payload": {
    "code": "invalid_event",
    "message": "hello requires request_id"
  }
}
```

## 配置结构建议

设备配置建议拆成这些大块：

- `network`
- `voice`
- `providers`
- `deck`
- `hid`
- `display`
- `features`
- `debug`

这样可以很自然地支持不同硬件能力组合。

## 动作抽象

不要把动作只建模成“发送快捷键”。

建议统一抽象：

- `hid_script`
- `hid_key_state`
- `open_url`
- `speak_text`
- `switch_ui_page`
- `update_led`
- `noop`

这样设备和智能体都能复用。

## 兼容策略

### 协议版本

保持：

- `v1`：当前稳定协议
- `v1beta`：实验字段

### 前向兼容

设备忽略自己不认识的字段。

### 后向兼容

服务端对低版本设备做字段裁剪。
