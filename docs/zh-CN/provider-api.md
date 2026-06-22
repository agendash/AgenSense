# Provider API

这份文档描述 AgenSense 第一版里的 API key 模式：如何注册 provider profile，如何在后续请求中直接复用这些配置调用 ASR / LLM / multimodal vision / TTS。

## 目标

AgenSense 现在既是设备兼容网关，也是共享 AI 感知服务。

对于 `AgenDash`、`AgenLeash` 和其他非 agen 系客户端，推荐优先使用 API key 模式，而不是设备 bootstrap 模式。

这条模式不要求客户端是硬件设备，也不要求提供 `device_id`。如果上层想带上自己的标识，可以通过 `client_id`、`device_label` 和 `session_id` 传入；其中 `device_label` 推荐放 `MacOS`、`Android`、`Web` 这类方便调试的客户端类型。

## 鉴权

所有 provider registry 和直接调用接口都使用：

```http
Authorization: Bearer <AGENSENSE_API_KEY>
```

这里的 `AGENSENSE_API_KEY` 代表一个调用方的身份边界。服务端不会直接保存原始 key，而是把它映射成稳定 namespace，用于隔离该调用方的 provider 配置。

## Provider Profile 存储

当前支持的 provider 字段：

- `asr_base_url`
- `asr_api_key`
- `asr_model`
- `llm_base_url`
- `llm_api_key`
- `llm_model`
- `multimodal_base_url`
- `multimodal_api_key`
- `multimodal_model`
- `tts_base_url`
- `tts_api_key`
- `tts_model`
- `vad_base_url`
- `vad_api_key`

这些配置会持久化到本地 `state.json` 中的 `provider_profiles`。

当前第一版里，上游 provider 的 `api_key` 会按原值保存在本地 JSON store。`AGENSENSE_API_KEY` 自身不会直接入库，而是先映射成稳定 namespace。

## 注册 Provider

### `POST /v1/providers`

请求体示例：

```json
{
  "id": "default",
  "name": "OpenAI Compatible Default",
  "asr_base_url": "http://127.0.0.1:8081/v1",
  "asr_api_key": "******",
  "asr_model": "whisper-1",
  "llm_base_url": "http://127.0.0.1:8081/v1",
  "llm_api_key": "******",
  "llm_model": "hauhaucs-qwen3.6-35b-a3b-aggressive-q4-k-m",
  "multimodal_base_url": "http://127.0.0.1:8081/v1",
  "multimodal_api_key": "******",
  "multimodal_model": "hauhaucs-qwen3.6-35b-a3b-aggressive-q4-k-m",
  "tts_base_url": "http://127.0.0.1:8081/v1",
  "tts_api_key": "******",
  "tts_model": "faster-qwen3-tts",
  "default": true
}
```

行为：

- 如果 `id` 不存在，则创建
- 如果 `id` 已存在，则覆盖更新
- 如果 `default=true`，则把它设为当前 API key namespace 下的默认 profile

## 查询 Provider

### `GET /v1/providers`

返回当前 API key namespace 下的所有 provider profiles。

### `GET /v1/providers/{id}`

返回单个 provider profile。

### `PATCH /v1/providers/{id}`

按相同结构更新单个 provider profile。

## 直接调用 API

### `POST /v1/asr/transcribe`

请求体示例：

```json
{
  "provider_profile_id": "default",
  "client_id": "agendash-desktop",
  "device_label": "MacOS",
  "session_id": "voice-001",
  "format": {
    "codec": "pcm_s16le",
    "sample_rate_hz": 16000,
    "channels": 1
  },
  "audio_base64": "AQIDBAU="
}
```

响应体示例：

```json
{
  "provider_profile_id": "default",
  "text": "..."
}
```

AgenSense 默认会把中文 ASR 转写归一成简体中文，避免上游模型偶尔输出繁体后影响后续 LLM / TTS。需要保留上游原文时，可以设置 `AGENSENSE_ASR_CHINESE_SCRIPT=original`；如果部署目标明确需要繁体，也可以设置为 `zh-Hant`。OpenAI-compatible ASR 请求会默认带上一段“中文使用简体”的提示词，可通过 `AGENSENSE_OPENAI_ASR_PROMPT` 覆盖，也可以用 `AGENSENSE_OPENAI_ASR_LANGUAGE` 传递 provider 支持的语言 hint。

### `POST /v1/llm/chat`

请求体示例：

```json
{
  "provider_profile_id": "default",
  "client_id": "agendash-desktop",
  "device_label": "MacOS",
  "session_id": "voice-001",
  "voice_assistant": {
    "contract": "universal_voice_layer_v1",
    "ui_context": {
      "current_scene": "chat",
      "focused_object": {
        "id": "session-alpha",
        "kind": "agent_session",
        "label": "alpha"
      }
    },
    "assistant_intent": {
      "scope": "focused_object",
      "target_id": "session-alpha",
      "action": "set_composer",
      "args": {"content": "hello"},
      "requires_confirmation": false,
      "ui_surface": "anchored_input"
    }
  },
  "messages": [
    {"role": "system", "content": "You are a concise assistant."},
    {"role": "user", "content": "hello"}
  ]
}
```

`voice_assistant`、`ui_context`、`assistant_intent` 和 `metadata` 都是可选字段。AgenDash 桌面端会优先使用嵌套的 `voice_assistant` envelope；为了兼容轻量客户端，也可以把 `ui_context` / `assistant_intent` 放在请求顶层。当前直接 LLM API 不会隐式改写 `messages`，客户端仍应把希望模型看到的上下文写入 messages；AgenSense 会保存这些字段用于 trace 和协议对齐。

响应体示例：

```json
{
  "provider_profile_id": "default",
  "text": "...",
  "deltas": ["...", "..."]
}
```

如果需要实时显示模型输出，可以使用 `POST /v1/llm/chat/stream`，请求体与 `/v1/llm/chat` 相同，返回 Server-Sent Events：

```text
event: delta
data: {"text":"partial text"}

event: done
data: {"provider_profile_id":"default","text":"full text","deltas":["partial text"]}
```

流式文本和 tool-use metadata 可以同时使用。当前 AgenSense 会流式转发模型文本，并把 Universal Voice Layer / MCP metadata 保存到 trace；真正执行工具、回填 tool result、再续写回答的闭环，仍应由客户端或后续工具运行时承接。

### `POST /v1/multimodal/chat`

这是通用 multimodal provider 接口，不绑定 Joyce。请求体示例：

```json
{
  "provider_profile_id": "default",
  "client_id": "agendash-desktop",
  "session_id": "vision-001",
  "messages": [
    {
      "role": "user",
      "content": [
        {"type": "text", "text": "这是什么？"},
        {
          "type": "image_url",
          "image_url": {
            "url": "data:image/png;base64,iVBORw0KGgo..."
          }
        }
      ]
    }
  ]
}
```

响应体示例：

```json
{
  "provider_profile_id": "default",
  "text": "..."
}
```

### `POST /v1/vision/analyze`

这是图片分析的便利接口，底层仍走同一套 multimodal provider。请求体示例：

```json
{
  "provider_profile_id": "default",
  "prompt": "描述这张 UI 截图，并指出视觉问题。",
  "images": [
    {
      "image_base64": "iVBORw0KGgo...",
      "mime_type": "image/png"
    }
  ]
}
```

如果 provider profile 没有填写 `multimodal_*` 字段，AgenSense 会回退使用同 profile 的 LLM base URL、API key 和 model。内置默认 profile 的 multimodal model 也默认继承 `AGENSENSE_DEFAULT_LLM_MODEL`；只有显式设置 `AGENSENSE_DEFAULT_MULTIMODAL_MODEL` 时才会分开。

### `POST /v1/tts/synthesize`

请求体示例：

```json
{
  "provider_profile_id": "default",
  "client_id": "agendash-desktop",
  "device_label": "MacOS",
  "session_id": "voice-001",
  "text": "hello",
  "format": {
    "codec": "pcm_s16le",
    "sample_rate_hz": 16000,
    "channels": 1
  }
}
```

响应体示例：

```json
{
  "provider_profile_id": "default",
  "format": {
    "codec": "pcm_s16le",
    "sample_rate_hz": 16000,
    "channels": 1
  },
  "audio_base64": "....",
  "chunk_count": 3
}
```

## Provider 选择规则

每次调用 direct-use API 时：

1. 如果请求明确带了 `provider_profile_id`，优先使用它
2. 否则尝试使用当前 API key namespace 下的默认 profile
3. 如果没有默认 profile 但仅存在一个 profile，则自动使用它
4. 如果存在多个 profile 且没有默认值，也没有显式指定，则返回错误

第一版默认启动时，会为 `demo-user-key` 对应 namespace 自动补齐 `default`。如果当前默认值还是旧的 `mock-default`，启动时也会自动切过去；已经显式切到其他 profile 的 namespace 不会被覆盖。

## 当前支持的 Provider 类型

当前第一版支持：

- `mock://...`
- OpenAI 兼容 HTTP provider

其中：

- ASR：调用 `/audio/transcriptions`
- LLM：调用 `/chat/completions` 的流式输出
- Multimodal：调用 `/chat/completions`，使用 OpenAI 风格的 `text` / `image_url` content parts
- TTS：调用 `/audio/speech`

推荐的 LocalAI `faster-qwen3-tts` 验证链路可以设置 `AGENSENSE_OPENAI_TTS_VOICE=Serena`。voice 支持取决于具体后端；如果后端拒绝 voice 字段，可以设置 `AGENSENSE_OPENAI_TTS_VOICE=none`。部分 LocalAI TTS 即使请求 PCM 也会返回 WAV 容器；AgenSense 会把 16-bit WAV 拆成 `pcm_s16le`，并回传真实采样率和声道数。

## 长连接说明

当前第一版：

- 客户端到 AgenSense 的设备链路可以是长连接 WebSocket
- 直接调用 API 是普通 HTTP 请求
- AgenSense 到上游 provider 目前依赖共享 `http.Client` 和 HTTP keep-alive
- 还没有实现常驻 provider WebSocket / gRPC 连接池
- 设备兼容模式下的 WebSocket 语音链路当前仍主要用于 mock 协议验证
