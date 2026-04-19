# 架构设计

## 设计原则

### 1. 设备要薄

ESP32-S3 负责：

- 音频采集与回放
- 本地输入事件
- USB HID
- 屏幕 / 触摸 / 旋钮 / 灯效
- 网络接入
- 少量本地缓存与降级逻辑

设备不负责：

- 模型 provider 细节
- 多服务编排
- 复杂重试与流控
- 多租户 / 多实例配置管理

### 2. 网关要统一

所有模型能力都通过网关统一抽象成：

- `speech-to-text`
- `chat`
- `text-to-speech`
- `vad`
- `tool / action`

这样设备协议不会被某个模型供应商绑死。

### 3. 高可用优先于过早微服务

第一阶段建议做模块化单体，而不是一开始拆成 6 个服务。理由很简单：

- 设备协议和会话编排还在快速变化。
- 微服务拆分会过早引入 RPC、链路追踪、跨服务幂等等复杂度。
- 你需要先把多硬件、多 provider、多配置模型的抽象做稳定。

所以建议先做“一套代码，多角色启动”：

- `--role=api`
- `--role=gateway`
- `--role=worker`
- `--role=scheduler`

后续如果某个角色压力明显独立，再拆。

## 逻辑分层

### 控制面

职责：

- 设备 bootstrap / claim / rebind
- 设备、实例、策略、provider profile 管理
- 配置版本控制
- 管理后台 API
- 运维和审计 API

### 实时网关面

职责：

- 设备 WebSocket 连接管理
- 设备鉴权
- 音频流收发
- 实时事件转发
- 配置 push
- 在线状态 / 心跳

### 编排层

职责：

- 把设备的语音会话编排成 `VAD -> ASR -> LLM -> TTS`
- 做 provider 选择、回退、超时和熔断
- 输出结构化动作，例如 HID action、UI hint、TTS 结果

### Provider 适配层

对接：

- OpenAI 兼容 LLM
- OpenAI 兼容 ASR
- 自建 TTS
- LocalAI / LM Studio
- 未来的专有服务

### 状态层

- PostgreSQL：持久配置与审计
- Redis / KeyDB：presence、config etag、分布式限流、会话临时态

## 核心实体

### Tenant

多项目隔离单位。未来不同硬件项目可以归属不同 tenant。

### Instance

一个可独立接入设备的逻辑实例。它定义：

- 默认 gateway URL
- provider profile
- 功能开关
- 地域 / 集群
- 配置模板

### Device

单个硬件终端。至少带这些字段：

- `device_id`
- `tenant_id`
- `instance_id`
- `hardware_sku`
- `chip_id`
- `mac_addr`
- `firmware_channel`
- `capabilities`
- `desired_config_version`
- `reported_config_version`

### Provider Profile

用于描述一组远程模型能力：

- 默认 ASR
- 默认 LLM
- 默认 TTS
- 可选 VAD 服务
- 各自 endpoint / api key / timeout / retry policy

### Policy

控制一个设备能做什么：

- 是否允许语音
- 是否允许 HID
- 是否允许远程动作
- 是否允许 4G / Wi-Fi / AP
- 是否强制某个 provider profile

## 会话流程

1. 设备先做 bootstrap，拿到自己的实例绑定关系和 token。
2. 设备连接 `gateway ws`，声明能力与当前配置版本。
3. 网关检查是否需要下发新配置。
4. 设备发起语音流。
5. 编排层根据实例 profile 调用远程 ASR / LLM / TTS。
6. 网关把流式结果回送设备。
7. 如需执行 HID / tool，网关发结构化 action 给设备。

## 配置分层

配置不要只做一层，建议分 5 层：

1. 固件编译默认值
2. 硬件 SKU 默认值
3. 实例模板配置
4. 设备覆盖配置
5. 会话临时覆盖

最终设备拿到的是 merge 后的快照，带版本号。

## 为什么这个架构适合 CM0

如果 CM0 只跑 gateway / relay，而不跑模型：

- CPU 压力主要来自 TLS、WebSocket、少量 JSON 编解码、音频转发
- 内存主要花在连接、缓冲区、少量缓存
- 不需要显存，也不需要大内存推理

这使得它可以作为边缘接入节点存在，但不适合承担控制面数据库和大规模 worker。
