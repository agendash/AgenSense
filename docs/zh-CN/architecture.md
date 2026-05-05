# 架构设计

## 当前定位

AgenSense 第一版的核心定位不是“某一类硬件的专用网关”，而是一个可共享的 AI 感知服务。

它优先服务两类调用方：

- `AgenDash` 这类 GUI 客户端
- `AgenLeash` 或其他服务端组件

设备 bootstrap / WebSocket 语音链路仍然保留，但现在属于兼容接入路径，不再是唯一主路径。

## 设计原则

### 1. 共享服务优先，设备兼容保留

第一优先级是让任意客户端只凭一个 `AGENSENSE_API_KEY` 就能：

- 注册自己的 provider profile
- 持久化保存这些配置
- 直接调用 ASR / LLM / TTS

硬件设备场景继续保留：

- `POST /v1/bootstrap`
- `GET /v1/device/config`
- `POST /v1/device/telemetry`
- `GET /v1/session/ws`

但这条路径主要解决设备协议、音频流和状态同步，不应该绑死整个项目的定位。

### 2. 客户端要薄

客户端负责：

- 音频采集与播放
- GUI / HID / 触摸 / 按键
- 会话上下文与用户交互
- 把文本、音频或控制请求发给 AgenSense

客户端不负责：

- 维护一堆上游 provider 适配逻辑
- 重试、超时、回退和连接复用
- provider 凭据存储
- 多 provider 混搭时的选择逻辑

### 3. Provider 抽象必须统一

模型能力要在服务端收敛成稳定接口：

- `speech-to-text`
- `chat`
- `text-to-speech`
- `vad`

这样 `AgenDash`、`AgenLeash`、硬件端和其他外部调用方都不需要直接耦合某个模型厂商的专有协议。

### 4. 第一版先做模块化单体

当前阶段优先稳定接口和边界，而不是过早拆服务。

第一版保留在一个进程里完成：

- HTTP API
- provider registry
- direct inference
- device compatibility gateway
- 本地文件存储

等 direct API、provider 适配和设备链路都稳定后，再决定是否拆成控制面、网关面和 worker 面。

## 当前逻辑分层

### Provider Registry 层

职责：

- 接收 `Authorization: Bearer <AGENSENSE_API_KEY>`
- 把 API key 映射成稳定 namespace
- 在 namespace 下保存和查询 provider profiles
- 记录默认 provider profile

这里的隔离边界是 API key，而不是硬件 `device_id`。

### Direct Inference 层

职责：

- 提供 `POST /v1/asr/transcribe`
- 提供 `POST /v1/llm/chat`
- 提供 `POST /v1/tts/synthesize`
- 根据显式 `provider_profile_id` 或默认 profile 解析上游 provider

这层是现在推荐给 `AgenDash`、`AgenLeash` 和其他普通客户端使用的主路径。

### Device Compatibility Gateway

职责：

- 处理 bootstrap、设备鉴权、配置回读
- 维护设备 WebSocket 会话
- 处理 `hello`、`audio.start` / binary / `audio.stop`
- 向下游回送 mock-friendly 的协议事件

当前第一版里，这条链路主要用于协议验证和硬件接入兼容，还没有与 provider registry 做统一编排。

### Provider Adapter 层

当前已经落地：

- `mock://` provider
- OpenAI 兼容 ASR
- OpenAI 兼容 LLM
- OpenAI 兼容 TTS

当前还没落地：

- VAD 运行时接口
- provider 健康检查
- 专门的长连接池或主动保活

### 状态层

第一版当前使用单文件 JSON store。

已经保存的核心数据：

- `provider_profiles`
- device / token / config snapshot

这意味着第一版更接近本地单机服务，而不是生产级多副本控制面。

## 核心实体

### API Key Namespace

第一版新的主隔离单位。

特点：

- 由 `AGENSENSE_API_KEY` 稳定映射而来
- 原始 API key 不直接入库存储
- namespace 下可保存多组 provider profile
- 同一个 API key 后续请求可直接复用已注册配置

### Provider Profile

描述一组可调用的上游能力：

- `asr_base_url` / `asr_api_key` / `asr_model`
- `llm_base_url` / `llm_api_key` / `llm_model`
- `tts_base_url` / `tts_api_key` / `tts_model`
- `vad_base_url` / `vad_api_key`

第一版中，上游 provider 的凭据当前直接保存在本地 `state.json`。

### Device

只在设备兼容路径中需要。

典型字段：

- `device_id`
- `tenant_id`
- `instance_id`
- `hardware_sku`
- `chip_id`
- `capabilities`
- `desired_config_version`
- `reported_config_version`

这类字段不应成为普通 GUI / 服务调用方的强制前提。

## 当前数据流

### 共享服务模式

1. 客户端带 `AGENSENSE_API_KEY` 调 `POST /v1/providers`
2. AgenSense 把配置保存到该 API key 对应的 namespace
3. 客户端后续调用 `/v1/asr/transcribe`、`/v1/llm/chat`、`/v1/tts/synthesize`
4. 服务端解析 profile，调用 `mock://` 或 OpenAI 兼容 provider
5. 结果返回给调用方

### 设备兼容模式

1. 设备做 bootstrap，拿到 `device_token`
2. 设备连接 `gateway ws`
3. 设备发起音频流
4. 当前网关走 mock pipeline 产出 `asr.final`、`llm.delta`、`tts.*`、`action.execute`

## 第一版边界

第一版已经落地：

- API key namespace 隔离
- provider profile 注册与持久化
- direct-use ASR / LLM / TTS API
- `mock://` 和 OpenAI 兼容 provider
- 设备 bootstrap / WebSocket 兼容链路

第一版明确还没做：

- VAD 运行时接口
- provider 凭据加密存储
- provider 主动保活和健康检查
- 多 API key 管理后台
- 配额、审计、细粒度限流
- 把设备 WebSocket 语音链路切换到统一的 provider registry 编排
