# Local MVP Runbook

这份文档是第一版 AgenSense 的本地操作清单，不是生产发布文档。

## 这轮 MVP 验证什么

第一版现在分成两条验证路径：

1. 共享服务模式：
   - 服务端启动成功
   - 用 `AGENSENSE_API_KEY` 注册 provider profile
   - 后续直接调用 ASR / LLM / TTS API
2. 设备兼容模式：
   - 设备可以 `POST /v1/bootstrap`
   - 设备可以建立 WebSocket 会话并发音频帧
   - 服务端可以回一组 mock-friendly 事件流

推荐优先验证第一条。第二条主要用于硬件接入和协议回归。

## 已确认的运行合同

当前已经从代码和测试里确认的事实：

- `go test ./...` 通过
- `go build ./cmd/agensense` 通过
- `internal/httpapi/router.go` 已经定义了：
  - `GET /healthz`
  - `POST /v1/bootstrap`
  - `GET /v1/device/config`
  - `POST /v1/device/telemetry`
  - `GET /v1/session/ws`
  - `GET/POST /v1/providers`
  - `GET/PATCH /v1/providers/{id}`
  - `POST /v1/asr/transcribe`
  - `POST /v1/llm/chat`
  - `POST /v1/tts/synthesize`
- `internal/service/provider_registry.go` 已经支持基于 API key namespace 的 provider profile 持久化
- `internal/service/inference.go` 已经支持 direct-use ASR / LLM / TTS
- `internal/provider` 当前已支持：
  - `mock://`
  - OpenAI 兼容 ASR / LLM / TTS
- `internal/gateway/handler_test.go` 已经端到端验证了设备 bootstrap + `hello` + 音频事件流
- `internal/store/file_repository.go` 已经提供单文件 JSON 持久化

当前还要明确两件事：

- provider profile 会存到本地 `state.json`
- 设备 WebSocket 语音链路当前仍主要是 mock pipeline，不等同于 direct-use provider API

## 启动顺序

```sh
go test ./...
go build ./cmd/agensense
go run ./cmd/agensense
```

如果你只想验证共享服务模式，建议关闭默认 demo 设备 seed：

```sh
AGENSENSE_DISABLE_DEMO_SEED=true \
AGENSENSE_LOG_LEVEL=debug \
go run ./cmd/agensense
```

如果你需要设备链路验证，可以保持默认启动，或显式指定地址：

```sh
AGENSENSE_ADDR=:18080 \
AGENSENSE_PUBLIC_BASE_URL=http://127.0.0.1:18080 \
AGENSENSE_LOG_LEVEL=debug \
go run ./cmd/agensense
```

日志说明：

- 默认输出到标准输出
- `AGENSENSE_LOG_LEVEL=debug` 可以打开更详细的调试日志
- `AGENSENSE_DEBUG=true` 可以打开 `/debug/*` trace 后台与音频资产采集；默认关闭
- 当前重点日志覆盖：启动、provider 注册、direct inference、设备 bootstrap、设备鉴权、WebSocket 会话
- 原始音频 binary frame 不会逐帧打印

## 共享服务模式验证

### 1. 健康检查

```sh
curl -sS http://127.0.0.1:8080/healthz
```

预期至少返回：

```json
{"ok":true}
```

### 2. 准备 API key

```sh
export AGENSENSE_API_KEY="demo-user-key"
```

### 3. 确认内置默认 provider

第一版启动后，会自动给 `demo-user-key` 这类默认 namespace 补上一套 `default`：

- `ASR`: `whisper-1`
- `LLM`: `hauhaucs-qwen3.6-35b-a3b-aggressive-q4-k-m`
- `TTS`: `faster-qwen3-tts`
- Host: `http://127.0.0.1:8081/v1`

默认 LocalAI 使用宿主机 `8081`，避免和 AgenSense 服务端口 `8080` 冲突。Docker Compose 全栈模式下，AgenSense 容器内使用 `http://localai:8080/v1`。

先确认它已经在：

```sh
curl -sS \
  http://127.0.0.1:8080/v1/providers \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}"
```

如果这里已经能看到 `default`，可以直接跳到后面的 ASR / LLM / TTS 验证。

### 4. 覆盖或注册 provider profile

如果你需要手工覆盖默认 profile，直接重新注册同一个 `id` 即可：

```sh
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
    "tts_base_url":"'"${PROVIDER_BASE_URL}"'",
    "tts_api_key":"'"${PROVIDER_API_KEY}"'",
    "tts_model":"faster-qwen3-tts",
    "default":true
  }'
```

然后确认它已经被持久化：

```sh
curl -sS \
  http://127.0.0.1:8080/v1/providers \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}"
```

### 4.1 OpenAI-compatible provider 示例

如果已有 OpenAI-compatible 上游，可以直接注册真实 profile。下面的模型名按你的上游服务实际支持情况替换。

示例：

```sh
export AGENSENSE_API_KEY="demo-user-key"
export PROVIDER_BASE_URL="http://127.0.0.1:8081/v1"
export PROVIDER_API_KEY="..."

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
    "tts_base_url":"'"${PROVIDER_BASE_URL}"'",
    "tts_api_key":"'"${PROVIDER_API_KEY}"'",
    "tts_model":"faster-qwen3-tts",
    "default":true
  }'
```

注意：

- 有些 OpenAI-compatible 服务在 `/v1/audio/speech` 返回的是 **WAV 容器**，即使请求里写了 `response_format=pcm`
- AgenSense 第一版已经在 TTS 响应里自动把这种情况标记为 `format.codec=wav`
- `AgenDash` 播放层也已对 WAV 头做兜底探测，因此这条链路现在可以直接用

### 5. 验证 LLM 直调

```sh
curl -sS \
  -X POST http://127.0.0.1:8080/v1/llm/chat \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}" \
  -H 'content-type: application/json' \
  -d '{
    "messages":[
      {"role":"system","content":"You are concise."},
      {"role":"user","content":"hello"}
    ]
  }'
```

预期返回：

- `provider_profile_id`
- `text`
- `deltas`

### 6. 验证 TTS 直调

```sh
curl -sS \
  -X POST http://127.0.0.1:8080/v1/tts/synthesize \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}" \
  -H 'content-type: application/json' \
  -d '{
    "text":"hello",
    "format":{
      "codec":"pcm_s16le",
      "sample_rate_hz":16000,
      "channels":1
    }
  }'
```

预期返回：

- `provider_profile_id`
- `format`
- `audio_base64`
- `chunk_count`

### 7. 验证 ASR 直调

随便准备一小段 base64 数据即可先验证默认 profile 已经被解析出来：

```sh
curl -sS \
  -X POST http://127.0.0.1:8080/v1/asr/transcribe \
  -H "Authorization: Bearer ${AGENSENSE_API_KEY}" \
  -H 'content-type: application/json' \
  -d '{
    "format":{
      "codec":"pcm_s16le",
      "sample_rate_hz":16000,
      "channels":1
    },
    "audio_base64":"AQIDBAU="
  }'
```

预期返回：

- `provider_profile_id`
- `text`

## AgenDash Voice Smoke 验证

这条链路用来在不启动 `AgenDash` 的情况下验证 `AgenDash voice mode` 依赖的服务端行为。它会先调用 `/v1/tts/synthesize` 生成一段真实的种子音频，再把这段音频按实时 PCM 流输入 `/v1/voice/ws`，然后等待并校验：

- `session.ready`
- `vad.state`
- `asr.final`
- `llm.delta`
- `llm.done`
- `tts.start`
- 下行 TTS binary frame
- `tts.stop`

## GUI Lite 验证

如果需要交互式检查，可以使用 [AgenSense GUI Lite](https://github.com/agendash/agensense-gui-lite)：

```sh
cd ../agensense-gui-lite
flutter run -d macos
```

它可以用来验证 provider 注册、直接 ASR / LLM / TTS、流式 ASR、实时 Voice WS、设备兼容接口以及 `/debug/api/traces`。

详细说明见 [GUI Lite 验证客户端](gui-lite.md)。
- `response.done`
- 如果服务端设置了 `AGENSENSE_DEBUG=true`，还会校验 `/debug/api/traces` 中对应的 voice trace 和音频资产

服务启动后直接运行：

```sh
go run ./cmd/agensense-smoke
```

默认行为：

- 目标服务：`http://127.0.0.1:8080`
- API key：`demo-user-key`
- provider profile：`smoke-mock`
- 会自动注册 `mock://asr`、`mock://llm`、`mock://tts`
- 输入音频：默认 `-input-source=tts`，即先用当前 provider 调 TTS 生成 seed audio，再流式送入 ASR/VAD
- 种子文本：`Please summarize the active workspace and say AgenSense smoke ok.`
- debug 校验：默认跟随 `AGENSENSE_DEBUG`；也可以显式传 `-expect-debug=true`
- 产物目录：`tmp/smoke/<session-id>`

产物目录里会保留：

- `seed_tts.pcm`
- `seed_tts.wav`
- `input.pcm`
- `input.wav`
- `tts.pcm`
- `tts.wav`
- `report.json`

如果只想快速跑协议和事件，不依赖 TTS 生成输入音频，可以改回 tone 模式：

```sh
go run ./cmd/agensense-smoke \
  -input-source=tone \
  -realtime=false
```

如果要验证当前默认真实 provider，而不是 mock profile：

```sh
go run ./cmd/agensense-smoke \
  -ensure-mock-provider=false \
  -provider-profile-id=default \
  -timeout=90s
```

如果只想快速跑协议和事件，不按真实麦克风节奏 sleep：

```sh
go run ./cmd/agensense-smoke -realtime=false
```

如果还要顺手验证 `agensense -> workspace/code agent` 的外部操作能力，可以加 AgenLeash 参数。默认 `-agenleash-message-mode=start-arg` 会把 smoke prompt 作为 `codex exec` 的启动参数传入，避免单独依赖消息 post：

```sh
go run ./cmd/agensense-smoke \
  -realtime=false \
  -agenleash-base-url=http://127.0.0.1:8081 \
  -agenleash-token=<AGENLEASH_TOKEN> \
  -agenleash-workspace="$(pwd)" \
  -agenleash-adapter=codex \
  -agenleash-wait=45s \
  -agenleash-message='Reply with exactly: AgenSense workspace smoke ok'
```

如果专门要压测 AgenLeash 的消息发送接口，可以显式切到 post 模式：

```sh
go run ./cmd/agensense-smoke \
  -agenleash-base-url=http://127.0.0.1:8081 \
  -agenleash-token=<AGENLEASH_TOKEN> \
  -agenleash-message-mode=post
```

## 设备兼容模式验证

这条路径是可选项。只有你需要验证设备 bootstrap、实时 WebSocket 协议或硬件集成时才需要跑。

### 1. Bootstrap

目标接口：

- `POST /v1/bootstrap`

默认 demo seed：

- `device_id`: `vdk-coreS3-001`
- `chip_id`: `esp32s3-abcdef`
- `hardware_sku`: `m5cores3-facekit-audio`
- `claim_token`: `factory-claim-token`
- `provider_profile_id`: `default`

示例请求体：

```json
{
  "device_id": "vdk-coreS3-001",
  "chip_id": "esp32s3-abcdef",
  "hardware_sku": "m5cores3-facekit-audio",
  "firmware_version": "1.2.0",
  "firmware_channel": "stable",
  "capabilities": {
    "display": "lcd",
    "touch": true,
    "usb_hid": true,
    "usb_mic": true,
    "cellular": false
  },
  "claim_token": "factory-claim-token"
}
```

调用：

```sh
curl -sS \
  -X POST http://127.0.0.1:8080/v1/bootstrap \
  -H 'content-type: application/json' \
  -d @bootstrap-request.json
```

响应里至少应该包含：

- `device_token`
- `ws_url`
- `config_version`
- `config`
- `device_id`

### 2. WebSocket

目标接口：

- `GET /v1/session/ws`

握手时至少要带：

- `Authorization: Bearer <device_token>`
- `X-Device-Id: <device_id>`
- `X-Protocol-Version: v1`

如果本地装了 `websocat`，可以先做 hello 验证：

```sh
websocat \
  -H="Authorization: Bearer <DEVICE_TOKEN>" \
  -H="X-Device-Id: <DEVICE_ID>" \
  -H="X-Protocol-Version: v1" \
  ws://127.0.0.1:8080/v1/session/ws
```

参考 `hello`：

```json
{
  "type": "hello",
  "request_id": "req-001",
  "payload": {
    "device": {
      "device_id": "vdk-coreS3-001",
      "hardware_sku": "m5cores3-facekit-audio",
      "firmware_version": "1.2.0",
      "capabilities": {
        "display": "lcd",
        "touch": true,
        "usb_hid": true,
        "usb_mic": true,
        "cellular": false
      }
    },
    "state": {
      "config_version": 1
    }
  }
}
```

然后继续验证：

1. 发 `audio.start`
2. 发一小段 binary audio frame
3. 发 `audio.stop`
4. 观察服务端是否按顺序回 mock 事件

参考控制消息：

```json
{
  "type": "audio.start",
  "payload": {
    "stream_id": "st-001",
    "codec": "pcm_s16le",
    "sample_rate_hz": 16000,
    "channels": 1
  }
}
```

```json
{
  "type": "audio.stop",
  "payload": {
    "stream_id": "st-001",
    "last_seq": 1
  }
}
```

这条链路当前的成功标准是：

- `hello.ok`
- `asr.final`
- 至少一个 `llm.delta`
- `llm.done`
- `tts.start`
- 至少一个下行 binary 音频块
- `tts.stop`
- `action.execute`

## 建议记录的验收证据

建议保留这些结果，方便后续回归：

- `go test ./...` 的通过记录
- `go build ./cmd/agensense` 的通过记录
- 一次 `go run ./cmd/agensense-smoke` 的通过记录和 `tmp/smoke/<session-id>/report.json`
- 一次成功的 provider 注册和查询结果
- 一次成功的 direct LLM / TTS / ASR 响应样例
- 如果验证了设备链路，再补一份 bootstrap 响应和 WebSocket 事件顺序

## 故意留到后面的内容

第一版先不补这些：

- 生产环境凭据加密存储
- provider 健康检查和主动保活
- 真正的本地 `docker compose`
- `systemd` unit
- k8s manifests
- 生产环境 token rotation / refresh 说明
