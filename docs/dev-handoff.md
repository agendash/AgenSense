# Dev Handoff

这份文档给下一个开发 session 直接开工用，基于当前第一版代码现实，而不是未来的理想形态。

## 当前第一版目标

第一版现在的主目标已经变成两条线并存：

1. 共享服务模式可用：
   - API key 可以隔离调用方
   - provider profile 可以注册、持久化、复用
   - 可以直接调用 ASR / LLM / TTS
2. 设备兼容模式保留：
   - bootstrap / config / telemetry / WebSocket 继续可用
   - 用于硬件协议回归和 mock-friendly 验证

不要再把项目只理解成“设备网关”。

## 当前代码已经有什么

### 入口和存储

- `cmd/agensense/main.go`
- `internal/store/file_repository.go`

当前第一版是单进程 + 本地 JSON store。

### HTTP API

- `GET /healthz`
- `POST /v1/bootstrap`
- `GET /v1/device/config`
- `POST /v1/device/telemetry`
- `GET /v1/session/ws`
- `GET/POST /v1/providers`
- `GET/PATCH /v1/providers/{id}`
- `POST /v1/asr/transcribe`
- `POST /v1/llm/chat`
- `POST /v1/tts/synthesize`

### 关键服务

- `internal/service/provider_registry.go`
  - 负责把 `AGENSENSE_API_KEY` 映射成稳定 namespace
  - 在 namespace 下存取 provider profile
  - 支持默认 profile 解析
- `internal/service/inference.go`
  - 提供 direct-use ASR / LLM / TTS
- `internal/provider/factory.go`
  - 根据 provider profile 构造 mock 或 OpenAI 兼容 provider client
- `internal/provider/openai_compatible.go`
  - 已经接上 OpenAI 兼容 ASR / LLM / TTS HTTP 接口

### 设备兼容链路

- `internal/gateway`
- `internal/protocol`
- `internal/session`

当前设备 WebSocket 语音链路还是接 mock pipeline，主要用于协议验证。不要误以为它已经自动复用了 provider registry。

## 当前边界

第一版已经明确支持：

- API key 维度的 provider profile 注册和复用
- `mock://` provider
- OpenAI 兼容 provider
- direct-use ASR / LLM / TTS API
- 设备 bootstrap / WebSocket 协议

第一版明确还没做：

- VAD 运行时接口
- provider 凭据加密存储
- provider 健康检查与主动保活
- 设备 WebSocket 语音链路切换到 registry/factory
- 多 API key 管理后台
- 审计、配额、细粒度限流

## 继续开发时的优先级

### 1. 先补共享服务，不要先做后台

优先级建议：

1. VAD 运行时接口
2. provider 健康检查
3. provider 凭据的更安全存储
4. direct-use API 的审计和限流

### 2. 再决定设备链路是否要切到统一编排

如果 `Agendash` 和其他 GUI 客户端主要走 direct-use API，那么设备 WebSocket 链路可以继续保留为兼容模式。

如果后面要让硬件设备也走真实 provider，而不是 mock pipeline，再把 `internal/gateway` 里的音频回路切到 `RegistryService + Factory`。

### 3. 不要过早引入重基础设施

当前先不要：

- 先拆微服务
- 先上消息队列
- 先做完整后台管理 UI
- 先做多协议大战

## 开发约束

- 业务层不要直接拼 OpenAI 兼容请求
- provider 访问统一走 `ASRClient` / `LLMClient` / `TTSClient`
- direct-use API 不强依赖 `device_id`
- 新增字段时优先考虑 API key namespace 语义，而不是硬件实体语义
- 文档统一使用“第一版”表述，不再另起所谓 `v2`
