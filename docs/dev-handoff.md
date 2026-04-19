# Dev Handoff

这份文档给下一个开发 session 直接开工用。

## 第一阶段目标

先做最小可用闭环，不做后台页面，不做复杂多 provider，不做高级观测。

闭环定义：

1. 设备可以 bootstrap。
2. 设备可以拿到 token 和实例配置。
3. 设备可以通过 WebSocket 建立会话。
4. 设备可以上传一段 PCM 音频。
5. 服务端可以调用一套远程 `ASR -> LLM -> TTS`。
6. 服务端可以把 TTS 音频流回设备。
7. 服务端可以下发一个 `action.execute`，设备能执行。

## 推荐技术栈

- Go 1.24+
- HTTP: `chi` 或 `gin`
- WebSocket: `nhooyr.io/websocket` 或 `gorilla/websocket`
- DB: PostgreSQL
- Cache: Redis
- Config: `koanf` 或纯环境变量 + yaml
- Logging: `zap`
- Metrics: `prometheus/client_golang`

## 包结构建议

```text
cmd/agensense
internal/bootstrap
internal/auth
internal/config
internal/device
internal/gateway
internal/provider
internal/provider/asr
internal/provider/llm
internal/provider/tts
internal/session
internal/store/postgres
internal/store/redis
internal/httpapi
```

## 第一批接口

### 控制面

- `POST /v1/bootstrap`
- `GET /v1/device/config`
- `POST /v1/device/telemetry`

### 实时面

- `GET /v1/session/ws`

## 第一批表

至少先建这些表：

- `tenants`
- `instances`
- `devices`
- `provider_profiles`
- `device_config_versions`

## 第一批数据结构

### Device

```go
type Device struct {
    ID                    string
    TenantID              string
    InstanceID            string
    HardwareSKU           string
    ChipID                string
    MACAddr               string
    FirmwareVersion       string
    FirmwareChannel       string
    DeviceTokenHash       string
    DesiredConfigVersion  int64
    ReportedConfigVersion int64
    CapabilitiesJSON      []byte
}
```

### ProviderProfile

```go
type ProviderProfile struct {
    ID              string
    TenantID        string
    ASRBaseURL      string
    ASRAPIKey       string
    ASRModel        string
    LLMBaseURL      string
    LLMAPIKey       string
    LLMModel        string
    TTSBaseURL      string
    TTSAPIKey       string
    TTSModel        string
    VADBaseURL      string
    VADAPIKey       string
}
```

## Provider 层约束

不要让业务代码直接拼 OpenAI 兼容请求。

定义统一接口：

```go
type ASRClient interface {
    Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResponse, error)
}

type LLMClient interface {
    ChatStream(ctx context.Context, req ChatRequest, cb func(ChatDelta) error) error
}

type TTSClient interface {
    SynthesizeStream(ctx context.Context, req TTSRequest, cb func(AudioChunk) error) error
}
```

## 第一批验收标准

### 本地验收

- 用 curl 能成功 bootstrap 一个假设备
- 用 WebSocket 客户端能完成 `hello`
- 上传一段 16k PCM 能拿到 `asr.final`
- 服务端能流式回 `tts` 音频块

### 设备验收

- ESP32 设备能拿到 bootstrap 配置
- 设备能连接 ws
- 设备能说一句话，收到一句 TTS

## 不要现在做的东西

- 不要现在做完整后台管理 UI
- 不要一开始上消息队列
- 不要先做多协议兼容
- 不要把 HID 细节写进网关内核

## 代码风格要求

- 领域对象和 provider client 分层
- 协议结构体单独定义
- 所有外部依赖都走接口
- 配置变更默认带版本号
