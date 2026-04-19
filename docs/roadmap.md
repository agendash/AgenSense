# Roadmap

## Milestone 0

文档先行，明确协议和边界。

## Milestone 1

实现控制面最小集合：

- bootstrap
- device claim
- instance / device / provider profile 数据模型
- config versioning

## Milestone 2

实现实时网关最小集合：

- `hello`
- `telemetry.update`
- `audio.start` / binary / `audio.stop`
- `config.snapshot`
- `action.execute`

## Milestone 3

接通一条最小可用 voice combo：

- 远程 ASR
- 远程 LLM
- 远程 TTS
- 流式回传

## Milestone 4

补齐生产必需项：

- token 刷新
- retry / timeout / breaker
- 审计日志
- 指标
- 基础后台

## Milestone 5

支持多实例、多项目、多硬件：

- CoreS3
- Xiaozhi Card
- 后续更多 ESP32-S3 板卡

## Milestone 6

边缘部署能力：

- ARM64 打包
- 低配 relay 模式
- 配置缓存
- 可控降级
