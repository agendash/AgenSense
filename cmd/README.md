# cmd

这里放 `agensense` 相关的可执行入口。

当前入口：

- `cmd/agensense`
- `cmd/agensense-smoke`

`agensense-smoke` 用于脱离 `Agendash` 做 voice WebSocket 全链路回归：

```sh
go run ./cmd/agensense-smoke
```

默认目标是 `http://127.0.0.1:8080`，会用 `demo-user-key` 创建或更新 `smoke-mock` provider profile，先调用 `/v1/tts/synthesize` 生成测试音频，再模拟 `Agendash` 推 PCM 音频流到 `/v1/voice/ws`，校验 VAD、ASR、LLM delta 和 TTS 二进制音频。服务端设置 `AGENSENSE_DEBUG=true` 时，smoke 也会校验 debug trace 音频资产；也可以显式传 `-expect-debug=true`。

常用变体：

```sh
# 绕过 seed TTS，直接用本地合成 tone，适合只测协议。
go run ./cmd/agensense-smoke -input-source=tone

# 使用当前 default profile，碰真实 ASR / LLM / TTS provider。
go run ./cmd/agensense-smoke -ensure-mock-provider=false -provider-profile-id=default -timeout=90s

# 顺手启动 Agenleash code agent，验证 workspace/code-agent API 被打通。
go run ./cmd/agensense-smoke \
  -agenleash-base-url=http://127.0.0.1:8081 \
  -agenleash-token=<AGENLEASH_TOKEN> \
  -agenleash-workspace="$(pwd)"
```
