# 开源方案参考

下面这些都可以参考，但我不建议直接把其中任何一个原样当成你的最终解。

## 1. xiaozhi-esp32-server

项目：

- <https://github.com/xinnan-tech/xiaozhi-esp32-server>

适合：

- 快速理解设备语音助手后端长什么样
- 参考设备接入、会话、配置和语音链路
- 与现有小智生态兼容

不适合直接当最终底座的原因：

- 语义和协议更偏小智自身
- 你后面要支持多种硬件和 HID / deck / 自定义 UI，抽象可能不够干净

结论：

值得重度参考，不建议直接照搬为最终多项目基座。

## 2. LiveKit Agents

官方：

- <https://docs.livekit.io/agents/>

适合：

- 做生产级实时语音 / 视频 agent
- 做更重的媒体编排
- 后面如果你要扩到浏览器、电话、RTC，会很强

局限：

- 它更偏 WebRTC / 媒体系统，不是为 ESP32 裸设备 WebSocket 接入量身定做
- 直接替代你的设备网关并不合适

结论：

适合作为后端 worker / media plane 参考，或者未来更重实时场景的后端，不适合作为你这个项目的设备接入层替身。

## 3. Pipecat

官方：

- <https://docs.pipecat.ai/>

适合：

- 快速搭语音 pipeline
- 多 provider 编排实验
- 智能体 prototype

局限：

- 更像 agent pipeline 框架
- 设备接入、bootstrap、多租户配置、实例管理不是它的强项

结论：

适合作为 worker 层的灵感来源，不适合作为完整设备网关。

## 我的建议

### 最稳的路线

- 你自己做 `agensense`
- 设备协议、bootstrap、配置中心、HID action 这些自己掌控
- provider 适配层尽量用 OpenAI-compatible 抽象

### 最现实的复用策略

- 参考 `xiaozhi-esp32-server` 的设备接入思路
- 借鉴 `LiveKit Agents` / `Pipecat` 的流式 agent 编排思路
- 但最终把“设备网关”和“模型编排”清晰分层
