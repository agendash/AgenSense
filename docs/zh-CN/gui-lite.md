# AgenSense GUI Lite

[AgenSense GUI Lite](https://github.com/agendash/agensense-gui-lite) 是一个轻量级 Flutter 验证客户端，用来在不启动完整 AgenDash 客户端的情况下测试 AgenSense。

它适合验证 provider profile、麦克风采集、直接 ASR / LLM / TTS API、实时语音 WebSocket、TTS 播放、设备兼容接口以及 debug trace。

## 推荐用法

先启动 AgenSense：

```sh
AGENSENSE_DEBUG=true go run ./cmd/agensense
```

再启动 GUI 客户端：

```sh
cd ../agensense-gui-lite
flutter run -d macos
```

默认连接参数：

- 服务地址：`http://127.0.0.1:8080`
- API key：`demo-user-key`
- Provider profile：`default`

## 可以测试什么

- Providers：查看和注册 LocalAI / OpenAI 兼容 provider profile
- LLM + Tool：测试 LLM 流式输出，并携带 Universal Voice Layer / MCP 风格上下文
- ASR：测试直接 ASR 和流式 ASR
- TTS：合成、播放和复听 TTS 音频
- Voice WS：测试麦克风 -> VAD -> ASR -> LLM -> TTS 播放的完整语音回路
- Device：检查 bootstrap、config、telemetry 和 session WebSocket 兼容性
- Debug：查看 `/debug/api/traces`

## TTS 声音

如果 TTS 后端支持 OpenAI 风格的 voice 参数，可以设置：

```sh
AGENSENSE_OPENAI_TTS_VOICE=Serena
```

如果某些 LocalAI 后端不接受 `voice` 参数，可以设置：

```sh
AGENSENSE_OPENAI_TTS_VOICE=none
```

当前 LocalAI 测试栈上的 `faster-qwen3-tts` 可以接受 `Serena` 这类命名声音，但不同后端的支持情况不完全一致。

## 移动设备

如果 Flutter 客户端运行在另一台手机或平板上，不要把服务地址写成 `127.0.0.1`，因为那表示设备自己。可以让 AgenSense 监听可访问地址：

```sh
AGENSENSE_ADDR=:8080 AGENSENSE_DEBUG=true go run ./cmd/agensense
```

然后在 GUI 中填写宿主机的局域网地址，例如：

```text
http://192.168.1.20:8080
```
