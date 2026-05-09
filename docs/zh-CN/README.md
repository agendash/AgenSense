# AgenSense

面向 `AgenDash`、`AgenLeash` 以及其他非 agen 客户端的可复用 AI 感知服务。

它的目标不是把 LLM / ASR / TTS / multimodal vision / VAD 跑在设备上，而是提供一层统一的网关与控制面，把不同模型服务收敛成一套稳定的协议和存储模型，供 GUI、硬件端和其他服务复用。

## 当前定位

AgenSense 现在同时支持两种接入方式：

- **共享服务模式**：客户端使用 `Authorization: Bearer <AGENSENSE_API_KEY>` 注册 provider profile，并直接调用 ASR / LLM / multimodal vision / TTS API
- **设备兼容模式**：保留原有 `bootstrap + device token + WebSocket` 路径，兼容 ESP32 / M5Stack / HID 类设备

第一种模式是现在推荐的主路径；第二种模式保留给硬件接入和本地 MVP 验证。

## 当前状态

到当前这个 checkout 为止，已经落下来的内容主要有：

- `cmd/agensense`：单进程可执行入口
- `internal/httpapi`：
  - `GET /healthz`
  - `POST /v1/bootstrap`
  - `GET /v1/device/config`
  - `POST /v1/device/telemetry`
  - `GET /v1/session/ws`
  - `GET/POST /v1/providers`
  - `GET/PATCH /v1/providers/{id}`
  - `POST /v1/asr/transcribe`
  - `POST /v1/llm/chat`
  - `POST /v1/multimodal/chat`
  - `POST /v1/vision/analyze`
  - `POST /v1/tts/synthesize`
- `internal/gateway`：设备 WebSocket 会话、`hello`、`audio.start` / binary / `audio.stop`
- `internal/protocol`：统一 `payload` envelope、事件类型和单流 `last_seq` 规则
- `internal/provider`：
  - `mock://` provider
  - OpenAI 兼容 ASR / LLM / multimodal / TTS 客户端
- `internal/device` + `internal/store`：provider profile、设备模型、token 逻辑、单文件 JSON 持久化 store

这意味着仓库现在已经不只是“设备网关 MVP”，也已经具备了共享 AI 感知服务的一版骨架。

## 本地验收入口

下面这些命令现在都应该通过：

```sh
go test ./...
go build ./cmd/agensense
go run ./cmd/agensense
```

如果要脱离 `AgenDash` 验证 voice mode 的全链路，可以在服务启动后跑：

```sh
go run ./cmd/agensense-smoke
```

这个 smoke runner 默认会先调用 `/v1/tts/synthesize` 生成一段测试语音，再把这段音频按麦克风流模拟 `AgenDash` 发送到 `/v1/voice/ws`，并校验 `VAD -> ASR -> LLM delta -> TTS binary`。如果服务端设置了 `AGENSENSE_DEBUG=true`，smoke 也会校验 debug trace 和音频资产。默认会创建一个独立的 `smoke-mock` provider profile，避免被本机真实 provider 状态干扰；如果要测真实 provider，可以关掉 `-ensure-mock-provider`。

如果需要手动验证 provider 注册、ASR / LLM / multimodal vision / TTS、实时 Voice WS、设备兼容接口和 debug trace，可以使用 [AgenSense GUI Lite](https://github.com/agendash/agensense-gui-lite)：

```sh
cd ../agensense-gui-lite
flutter run -d macos
```

详细流程见 [GUI Lite 验证客户端](gui-lite.md)。

如果还要把识别结果继续打到本机 `AgenLeash`，启动 code agent 并验证 workspace API，可以加：

```sh
go run ./cmd/agensense-smoke \
  -agenleash-base-url=http://127.0.0.1:8081 \
  -agenleash-token=<AGENLEASH_TOKEN> \
  -agenleash-workspace="$(pwd)"
```

更具体的启动、provider 注册和 WebSocket 验证步骤见：

- [本地运行手册](mvp-local-runbook.md)
- [Provider API](provider-api.md)
- [发布流程](release.md)

## Debug 后台

默认关闭。启动服务时显式设置 `AGENSENSE_DEBUG=true` 后，可以打开：

- `http://127.0.0.1:8080/debug/traces`

这个页面会显示最近的 trace，并提供对应的 JSON / 音频资产接口：

- `GET /debug/api/traces`
- `GET /debug/api/traces/{id}`
- `GET /debug/assets/{id}/input.wav`
- `GET /debug/assets/{id}/tts.wav`

当前会记录的内容包括：

- 输入音频
- ASR 文本与耗时
- 发给 LLM 的 messages
- LLM deltas、首字延迟、完整返回文本
- TTS 输入文本、首包延迟、输出音频
- 整轮 timeline

这套 debug 后台主要用于排查“为什么只听到零碎 TTS”“为什么首字很慢”“到底是哪一段阻塞了”这类问题。

`AGENSENSE_DEBUG` 只控制 trace 后台与音频资产采集；`AGENSENSE_LOG_LEVEL=debug` 只控制日志详细程度。

## 默认运行参数

默认情况下：

- 监听地址：`127.0.0.1:8080`
- `AGENSENSE_ADDR`：覆盖监听地址
- `AGENSENSE_PUBLIC_BASE_URL`：覆盖 bootstrap 返回的 `ws_url`
- `AGENSENSE_DATA_DIR`：覆盖本地 JSON store 目录，默认是 `tmp/agensense`
- `AGENSENSE_LOG_LEVEL`：日志级别，支持 `debug`、`info`、`warn`、`error`
- `AGENSENSE_DEBUG=true`：打开 `/debug/*` trace 后台与音频资产采集，默认关闭
- `AGENSENSE_DISABLE_DEMO_SEED=true`：关闭默认 demo 设备 seed

启动时还会自动确保一套共享服务默认 provider：

- 默认 API key namespace：`demo-user-key`
- 默认 profile id：`default`
- 默认上游：`http://127.0.0.1:8081/v1`
- 默认模型：
  - ASR：`whisper-1`
  - LLM：`hauhaucs-qwen3.6-35b-a3b-aggressive-q4-k-m`
  - Multimodal：默认继承 LLM 模型
  - TTS：`faster-qwen3-tts`

AgenSense 默认假设本机 LocalAI 监听在 `127.0.0.1:8081`，避免和 AgenSense 自己的 `8080` 端口冲突。Docker Compose 全栈模式下，AgenSense 容器会通过 `http://localai:8080/v1` 访问 LocalAI。

如果当前 namespace 还没有默认 profile，或者默认值仍然停在旧的 `mock-default`，启动时会自动切到这套内置 LocalAI 配置。已经显式切到其他 profile 的 namespace 不会被强制覆盖。

日志当前输出到标准输出，默认覆盖：

- 进程启动与停止
- HTTP 请求
- provider 注册和直接调用
- 设备 bootstrap 与设备鉴权
- WebSocket 会话建立与关键事件
- mock / provider 调用开始与完成

原始音频帧不会逐条打印。

## 共享服务模式怎么用

### 1. 选择一个 API key

客户端自行持有一个 API key，并在请求时通过：

```sh
Authorization: Bearer <AGENSENSE_API_KEY>
```

访问 AgenSense。

同一个 API key 对应一个稳定的内部 namespace。注册过的 provider profile 会被持久化到这个 namespace 下，后续同一 API key 可直接复用。

这里的“客户端”不要求是硬件设备，也不要求提前分配 `device_id`。对 `AgenDash`、`AgenLeash` 或其他普通 GUI / 服务调用方来说，只需要持有一个 API key 即可。

### 2. 注册 provider profile

对于默认的 `demo-user-key`，服务启动后就会直接带上一套 `default`。如果你只是想直接使用，可以先查询：

```sh
export AGENSENSE_API_KEY="demo-user-key"

curl -sS \
  http://127.0.0.1:8080/v1/providers \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}"
```

如果你要覆盖它，再调用注册接口即可。示例：

```sh
export AGENSENSE_API_KEY="demo-user-key"
export PROVIDER_BASE_URL="http://127.0.0.1:8081/v1"
export PROVIDER_API_KEY="replace-me"

curl -sS \
  -X POST http://127.0.0.1:8080/v1/providers \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}" \
  -H 'content-type: application/json' \
  -d '{
    "id":"default",
    "name":"OpenAI Compatible Default",
    "asr_base_url":"'"${PROVIDER_BASE_URL}"'",
    "asr_api_key":"'"${PROVIDER_API_KEY}"'",
    "asr_model":"whisper-1",
    "llm_base_url":"'"${PROVIDER_BASE_URL}"'",
    "llm_api_key":"'"${PROVIDER_API_KEY}"'",
    "llm_model":"hauhaucs-qwen3.6-35b-a3b-aggressive-q4-k-m",
    "multimodal_base_url":"'"${PROVIDER_BASE_URL}"'",
    "multimodal_api_key":"'"${PROVIDER_API_KEY}"'",
    "multimodal_model":"hauhaucs-qwen3.6-35b-a3b-aggressive-q4-k-m",
    "tts_base_url":"'"${PROVIDER_BASE_URL}"'",
    "tts_api_key":"'"${PROVIDER_API_KEY}"'",
    "tts_model":"faster-qwen3-tts",
    "default":true
  }'
```

### 3. 直接调用 ASR / LLM / Multimodal / TTS

注册一次后，后续可以直接使用：

- `POST /v1/asr/transcribe`
- `POST /v1/llm/chat`
- `POST /v1/multimodal/chat`
- `POST /v1/vision/analyze`
- `POST /v1/tts/synthesize`

这些接口默认会：

- 优先使用请求里指定的 `provider_profile_id`
- 未指定时尝试使用该 API key 下的默认 profile
- 若没有默认 profile 且只存在一个 profile，则自动选中它

### 4. 关于长连接

- AgenSense 当前会保持**客户端到服务端**的设备 WebSocket 长连接
- 对上游 ASR / LLM / multimodal / TTS provider，当前第一版使用共享 `http.Client` 做请求复用，依赖 HTTP keep-alive
- 当前没有实现专门的“常驻 provider WebSocket / gRPC 长连接池”
- provider profile 中保存的上游 `api_key` / `base_url` 当前会持久化到本地 `state.json`

## 设备兼容模式怎么用

设备兼容模式仍然保留：

- `POST /v1/bootstrap`
- `GET /v1/device/config`
- `POST /v1/device/telemetry`
- `GET /v1/session/ws`

这条路径适合：

- ESP32 / M5Stack / HID 设备接入
- 本地 mock-friendly 语音链路验收
- 需要 `audio.start` / binary / `audio.stop` 的设备协议场景

当前第一版里，这条设备兼容路径仍然主要接在 mock pipeline 上，用来验证协议、音频帧和事件流；真正按 API key namespace 选 provider 并直接调用上游模型，优先走共享服务 API。

默认 demo 设备：

- `device_id`: `vdk-coreS3-001`
- `claim_token`: `factory-claim-token`
- `chip_id`: `esp32s3-abcdef`
- `hardware_sku`: `m5cores3-facekit-audio`
- `provider_profile_id`: `default`

如果你不需要这条模式，可以用 `AGENSENSE_DISABLE_DEMO_SEED=true` 关闭默认 seed。

## 第一版的边界

当前第一版已经支持：

- 基于 API key 的 provider profile 注册和持久化
- 基于 API key 的直接 ASR / LLM / multimodal vision / TTS 调用
- 设备兼容模式的 bootstrap + WebSocket 会话
- `mock://` provider
- OpenAI 兼容 ASR / LLM / multimodal / TTS provider

当前第一版暂未补齐：

- VAD 运行时调用接口
- 多 API key 管理后台
- provider 健康检查和主动保活
- 更细粒度的配额、审计和限流
- 设备 WebSocket 语音链路与 provider registry 的统一编排

## 核心定位

- 客户端 / 设备侧保留轻量能力：音频采集、UI、按键 / 触摸 / HID、网络接入
- AgenSense 统一处理：provider 配置存储、API key 隔离、ASR / LLM / multimodal vision / TTS 编排、设备协议兼容
- 模型侧全部远程化：可以接 mock、OpenAI 兼容 API、自建 ASR / TTS / LLM 服务

## 为什么单独拆一个项目

如果每个客户端都直接各自配置 `ASR URL + LLM URL + TTS URL`，长期会遇到几个问题：

- provider 配置分散在各客户端，难以统一维护
- 切换后端或增加模型路由时，所有客户端都要改
- 多客户端、多硬件场景下，重试、熔断、超时和鉴权会重复实现
- `AgenDash`、`AgenLeash` 和其他非 agen 系客户端不应该重复造一套感知层

## 长期目标

- 一套统一的共享感知服务接口
- 一套统一的设备实时协议
- 支持设备主动 bootstrap / claim / rebind
- 支持多 provider 适配层，ASR / LLM / multimodal / TTS / VAD 可混搭
- 支持边缘 relay 模式，便于以后裁到低配 ARM 设备上跑

## 目录

- [本地运行手册](mvp-local-runbook.md)：本地运行、provider 注册与设备链路验证
- [Provider API](provider-api.md)：API key 模式和 provider API 说明
- [架构设计](architecture.md)：总体架构方向
- [设备 Bootstrap](device-bootstrap.md)：设备自注册 / 远程配置设计
- [实时协议](protocol.md)：设备实时协议设计
- [部署](deployment.md)：部署方式、Docker Compose 和脚本
- [发布流程](release.md)：GitHub Release、GoReleaser 和 Homebrew tap
- [LocalAI 配置](localai.md)：LocalAI 配置和默认地址说明
- [高可用与部署建议](deployment-ha.md)：高可用与部署建议
- [开源方案参考](open-source-options.md)：可参考或复用的开源方案
- [路线图](roadmap.md)：建议实施顺序
- [开发交接](dev-handoff.md)：后续开发落地清单
