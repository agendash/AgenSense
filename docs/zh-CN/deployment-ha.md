# 高可用说明

这份文档记录 AgenSense 后续高可用方向。当前代码库仍然是本地优先的单进程服务。

## 推荐拓扑

### 控制面

- HTTP API 节点
- provider registry
- 配置管理
- 审计和指标

### 网关面

- WebSocket gateway 节点
- voice session 入口
- 设备兼容会话

### Worker 面

- provider 编排
- ASR / LLM / TTS / 后续 VAD 调用
- retry 和 fallback 处理

### 共享基础设施

后续生产部署应该补：

- PostgreSQL 或其他持久数据库，用于设备和 provider 状态
- Redis / KeyDB，用于 presence、限流和短生命周期 session 元数据
- 结构化日志和指标采集

## Gateway 状态

WebSocket 连接本身会驻留在某一个 gateway 节点上，但 gateway 不应该只在内存里保存关键状态。

适合放到共享层的状态包括：

- device presence
- 当前配置版本
- 幂等键
- 限流计数
- 重连提示

## 重连模型

设备和客户端应该可以干净重连：

- 打开新的 WebSocket
- 发送 `hello`
- 获取或接收最新 config snapshot
- 开始新的 audio stream

第一阶段不要求会话迁移。

## Provider 韧性

ASR / LLM / TTS provider 是最容易波动的外部依赖。

生产编排至少需要：

- request timeout
- 有边界的 retry/backoff
- circuit breaker
- provider health check
- fallback profile
- provider 延迟和错误率指标

## Edge Relay 模式

Edge relay 是给受限场地使用的轻量部署形态。

建议约束：

- 不做本地模型推理
- 不持有主状态
- 不运行重型 metrics 栈
- 可选只读 config cache
- 上游 provider 调用继续走远端

Edge relay 应被视为可重连 gateway，而不是 source of truth。

## 运维信号

最低限度指标：

- 在线设备数
- 活跃 voice session 数
- bootstrap 成功率
- provider 成功率
- provider p50 / p95 延迟
- config ACK 延迟
- 重连率

常用日志字段：

- `tenant_id`
- `instance_id`
- `device_id`
- `session_id`
- `provider_profile_id`
- `request_id`

## 升级策略

- 先升级控制面节点
- 再升级 worker
- 最后升级 gateway
- 新协议字段必须可选且可忽略
- 配置改动必须携带显式版本号

## 消息总线建议

在 gateway/provider 边界证明确实需要之前，不要过早引入 Kafka、NATS 或其他 broker。

第一阶段 Redis 足够覆盖：

- presence
- rate limit
- config version cache
- 短生命周期协调
