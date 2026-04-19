# 高可用与部署建议

## 先说结论

如果模型全部远程化，CM0 可以考虑承担“边缘接入 / relay / gateway”角色，但不适合做完整生产控制面。

原因不是功能做不到，而是冗余和可维护性太差：

- 只有 512MB 内存余量很紧
- 数据库、缓存、观测一旦都挤进去，稳定性会很差
- 一出问题没有容错空间

所以建议分两层：

- 中心集群：标准生产环境
- 边缘节点：轻量 relay

## 推荐拓扑

### 中心集群

- `lb`
- `api x2`
- `gateway x2`
- `worker x2`
- 托管 `PostgreSQL`
- 托管 `Redis / KeyDB`

### 边缘节点

- `gateway x1`
- 可选本地只读配置缓存
- 所有模型请求继续转发到中心 provider 或云 provider

## 单二进制多角色

建议一套二进制支持不同角色：

- `agensense api`
- `agensense gateway`
- `agensense worker`
- `agensense scheduler`

这样有几个好处：

- 开发阶段快
- 部署阶段灵活
- 压力拆分清楚
- 以后要拆服务也平滑

## HA 关键点

### 1. 网关尽量无状态

WebSocket 连接本身会驻留在单个节点上，但节点不应该保存难以恢复的关键状态。

要把这些放进共享层：

- 设备 presence
- 当前配置版本
- 幂等键
- 限流计数

### 2. 允许重连恢复

设备天然会断网，所以协议必须支持：

- 自动重连
- 重新 `hello`
- 增量同步配置
- 中断后重新开始一轮语音会话

不要做“必须会话漂移”的复杂机制，第一阶段不值这个复杂度。

### 3. 配置下发要幂等

任何 `config.snapshot` / `config.patch` 都必须带版本号和 ACK。

### 4. provider 调用要有熔断

ASR / LLM / TTS 是最容易不稳定的外部依赖。至少要有：

- timeout
- retry
- circuit breaker
- fallback profile

## 资源建议

下面是保守估算，不是硬性上限。

### CM0 边缘 relay 模式

前提：

- 不做本地模型推理
- 不做音频转码
- 不做录音落盘
- 不跑 Prometheus 全量 metrics

建议目标：

- 在线设备：20 到 50 台
- 同时活跃语音会话：1 到 5 路

如果要更稳，建议直接上更高配 ARM 板卡，不要把 CM0 当生产核心节点。

### 中心节点起步规格

- `api/gateway`：2 vCPU / 2GB RAM
- `worker`：2 到 4 vCPU / 4GB RAM

这是假设模型全部远程化、worker 只是做编排和 HTTP / WS 客户端。

## 运维建议

### 最低限度观测

- 在线设备数
- 活跃语音会话数
- bootstrap 成功率
- provider 调用成功率
- provider 延迟 p50 / p95
- 配置下发 ACK 延迟
- 设备重连率

### 日志结构

每条日志至少带：

- `tenant_id`
- `instance_id`
- `device_id`
- `session_id`
- `provider`
- `request_id`

### 升级策略

- 先控制面，再 worker，再 gateway
- 设备协议字段新增必须可忽略
- 所有配置改动默认带版本号

## 为什么不建议一开始就上 NATS / Kafka

因为第一阶段最难的是设备协议和 provider 适配，不是内部事件总线。

你可以把 Redis 先用起来：

- presence
- rate limit
- config version cache

真到了跨角色异步任务明显变复杂，再引入消息总线。
