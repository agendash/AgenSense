# Roadmap

## Milestone 0

文档先行，明确协议和边界。

## Milestone 1

先把共享服务模式打稳：

- API key namespace
- provider profile 注册 / 查询 / 默认值
- direct-use ASR / LLM / TTS API
- 本地 JSON store 持久化

## Milestone 2

保留设备兼容模式：

- bootstrap
- device config / telemetry
- `hello`
- `telemetry.update`
- `audio.start` / binary / `audio.stop`
- `config.snapshot`
- `action.execute`

## Milestone 3

把 provider runtime 做完整：

- VAD runtime
- provider 健康检查
- retry / timeout / breaker
- direct-use API 的更稳定编排

## Milestone 4

补齐生产必需项：

- 凭据安全存储
- 审计日志
- 指标
- 配额 / 限流

## Milestone 5

扩展接入面：

- `AgenDash`
- `AgenLeash`
- 第三方非 agen 客户端
- 硬件设备继续兼容接入

## Milestone 6

边缘部署能力：

- ARM64 打包
- 低配 relay 模式
- 配置缓存
- 可控降级
