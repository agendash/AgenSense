# agensense

面向 ESP32 / M5Stack / e-paper / HID 设备的可复用语音网关项目。

这个项目不在设备上跑 LLM / ASR / TTS / VAD 模型本身，而是提供一层统一的实时网关与控制面，把多家远程模型服务收敛成一套稳定协议，给多种前端设备复用。

## 当前状态

这个仓库最初只有设计文档。到当前这个 checkout 为止，已经落下来的内容主要有：

- `cmd/agensense`：单进程可执行入口
- `internal/httpapi`：`/healthz`、`/v1/bootstrap`、`/v1/device/config`、`/v1/device/telemetry`
- `internal/gateway`：设备 WebSocket 会话、`hello`、`audio.start` / binary / `audio.stop`
- `internal/protocol`：统一 `payload` envelope、事件类型和单流 `last_seq` 规则
- `internal/provider/mock` + `internal/session`：确定性的 mock `ASR -> LLM -> TTS -> action`
- `internal/device` + `internal/store`：领域模型、token 逻辑、demo seed、单文件 JSON 持久化 store

这意味着仓库现在已经是一个可本地启动、可本地验收的 MVP，而不是只有设计文档。

## 本地验收入口

下面这些命令现在都应该通过：

```sh
go test ./...
go build ./cmd/agensense
go run ./cmd/agensense
```

更具体的启动、bootstrap 和 WebSocket 验证步骤见 [`docs/mvp-local-runbook.md`](/Users/zhuzhe/Workspace/agen/agensense/docs/mvp-local-runbook.md)。

## 默认运行参数

默认情况下：

- 监听地址：`127.0.0.1:8080`
- `AGENSENSE_ADDR`：覆盖监听地址
- `AGENSENSE_PUBLIC_BASE_URL`：覆盖 bootstrap 返回的 `ws_url`
- `AGENSENSE_DATA_DIR`：覆盖本地 JSON store 目录，默认是 `tmp/agensense`
- `AGENSENSE_LOG_LEVEL`：日志级别，支持 `debug`、`info`、`warn`、`error`，默认是 `info`

日志当前输出到标准输出，默认会覆盖：

- 进程启动与停止
- HTTP 请求
- 自注册 / bootstrap
- 设备鉴权
- WebSocket 会话建立与关键事件
- mock `ASR` / `LLM` / `TTS` 请求与完成

原始音频帧不会逐条打印。

默认 demo 设备：

- `device_id`: `vdk-coreS3-001`
- `claim_token`: `factory-claim-token`
- `chip_id`: `esp32s3-abcdef`
- `hardware_sku`: `m5cores3-facekit-audio`

## 这轮 MVP 不打算做什么

- 不接入真实生产级 ASR / LLM / TTS 凭据作为默认路径
- 不要求外部数据库、缓存或消息队列
- 不做后台管理 UI
- 不做多节点 HA 验证
- 不把设备 UI / HID 业务逻辑塞进网关内核

## 核心定位

- 设备侧只保留轻量能力：音频采集、VAD 前处理、UI、按键 / 触摸 / 编码器、USB HID、网络接入
- 网关侧统一处理：设备认证、会话编排、配置下发、模型路由、流式 ASR / LLM / TTS 串联、状态观测
- 模型侧全部远程化：可以接 LocalAI、LM Studio、OpenAI 兼容 API、自建 TTS / ASR 服务

## 为什么单独拆一个项目

直接让每个设备分别配置 `ASR URL + LLM URL + TTS URL` 可以工作，但长期会遇到几个问题：

- 设备协议会和模型供应商 API 强耦合，后面切后端很痛
- 多设备场景里，鉴权、超时、重试、熔断、流控、模型回退会重复实现
- 你后续有多个硬件项目，要复用同一套 voice combo 服务，不应该把“编排逻辑”烘进每个固件
- 设备主动绑定到自己的实例、远程改配置、灰度切流、按设备能力分配模型，这些都更适合在网关层做

## 长期目标

- 一套统一的设备实时协议，支持 ESP32-S3、CoreS3、Xiaozhi Card 等前端
- 支持设备主动 bootstrap / claim / rebind，自行拿到所属实例配置
- 支持 HA 部署，网关节点可水平扩容
- 支持多 provider 适配层，ASR / LLM / TTS / VAD 可混搭
- 支持边缘 relay 模式，便于以后裁到低配 ARM 设备上跑

## 长期架构方向

下面这些是长期方向，不是这轮本地 MVP 的实际交付形态：

- 语言：Go
- 架构：模块化单体，多角色进程
- 角色：
  - `api`：控制面 / 管理面 / bootstrap
  - `gateway`：设备 WebSocket 会话与实时流转
  - `worker`：ASR / LLM / TTS / VAD 适配与编排
  - `scheduler`：配置推送、健康检查、异步任务
- 共享组件：
  - PostgreSQL：设备、实例、配置、审计
  - Redis / KeyDB：会话态、presence、限流、幂等键

## 长期部署方向

### 1. 标准生产模式

- 入口层：Nginx / Envoy / HAProxy
- 网关层：多个 `gateway` 实例
- 控制面：多个 `api` 实例
- 数据层：托管 PostgreSQL + Redis
- 模型层：远程 provider

### 2. 边缘 Relay 模式

- 单个二进制只开启 `gateway` 角色
- 不做本地推理
- 只负责设备接入、认证、配置缓存、转发到远端模型 provider
- 适合以后跑在低配 ARM 板卡上

## 目录

- [`docs/mvp-local-runbook.md`](/Users/zhuzhe/Workspace/agen/agensense/docs/mvp-local-runbook.md)：这轮本地 MVP 的启动与验收清单
- [`docs/architecture.md`](/Users/zhuzhe/Workspace/agen/agensense/docs/architecture.md)：总体架构方向
- [`docs/device-bootstrap.md`](/Users/zhuzhe/Workspace/agen/agensense/docs/device-bootstrap.md)：设备自注册 / 远程配置设计
- [`docs/protocol.md`](/Users/zhuzhe/Workspace/agen/agensense/docs/protocol.md)：设备实时协议设计
- [`docs/deployment-ha.md`](/Users/zhuzhe/Workspace/agen/agensense/docs/deployment-ha.md)：高可用与部署建议
- [`docs/open-source-options.md`](/Users/zhuzhe/Workspace/agen/agensense/docs/open-source-options.md)：可参考或复用的开源方案
- [`docs/roadmap.md`](/Users/zhuzhe/Workspace/agen/agensense/docs/roadmap.md)：建议实施顺序
- [`docs/dev-handoff.md`](/Users/zhuzhe/Workspace/agen/agensense/docs/dev-handoff.md)：后续开发落地清单

## 后续阶段建议

优先顺序应该是：

1. 把 mock provider 替换成真实 provider 适配层
2. 把 file store 扩展成 Postgres / Redis 版本
3. 增加更完整的错误码、指标、审计和 health checks
4. 再往多 provider、灰度和 HA 部署推进
