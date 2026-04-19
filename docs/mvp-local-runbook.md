# Local MVP Runbook

这份文档是这轮本地 MVP 的操作清单，不是生产发布文档。

## 这轮 MVP 只验证什么

目标仍然是一条本地、mock-friendly 的最小闭环：

1. 服务端可以启动成单进程
2. 设备可以 `POST /v1/bootstrap`
3. 设备可以拿到 `device_token`、`ws_url`、配置快照或配置版本
4. 设备可以建立 WebSocket 会话并完成 `hello`
5. 设备可以发 `audio.start`、binary audio frame、`audio.stop`
6. 服务端会回一组 mock 事件：
   - `asr.final`
   - `llm.delta`
   - `llm.done`
   - `tts.start`
   - binary audio frame
   - `tts.stop`
   - `action.execute`

这轮不验证真实 provider、HA、多节点路由、管理后台或生产级 token 生命周期。

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
- `internal/protocol` 已经把 JSON 控制消息定成统一 envelope：
  - 所有事件都必须带 `payload`
  - `hello` 也必须走 `payload.device` / `payload.state`
  - legacy 的扁平 `hello` 形状会被拒绝
- `internal/session` + `internal/provider/mock` 已经能在包级别跑通一轮 mock `ASR -> LLM -> TTS -> action`
- `internal/gateway/handler_test.go` 已经端到端验证了：
  - bootstrap
  - `hello.ok`
  - `config.snapshot`
  - `audio.start` / binary / `audio.stop`
  - `asr.final`
  - `llm.delta`
  - `llm.done`
  - `tts.start`
  - 下行 binary 音频块
  - `tts.stop`
  - `action.execute`
- `internal/device/seed.go` 已经定义了 demo seed 的默认值：
  - `device_id`: `vdk-coreS3-001`
  - `chip_id`: `esp32s3-abcdef`
  - `hardware_sku`: `m5cores3-facekit-audio`
  - `claim_token`: `factory-claim-token`
  - `ws_url`: `ws://127.0.0.1:8080/v1/session/ws`
- `internal/store/file_repository.go` 已经提供单文件 JSON 持久化

## 启动顺序

```sh
go test ./...
go build ./cmd/agensense
go run ./cmd/agensense
```

如果你不想占用默认端口，可以这样起：

```sh
AGENSENSE_ADDR=:18080 \
AGENSENSE_PUBLIC_BASE_URL=http://127.0.0.1:18080 \
AGENSENSE_LOG_LEVEL=debug \
go run ./cmd/agensense
```

日志说明：

- 默认输出到标准输出
- `AGENSENSE_LOG_LEVEL=debug` 可以打开更详细的调试日志
- 当前重点日志覆盖：启动、自注册/bootstrap、设备鉴权、WebSocket 会话、ASR/LLM/TTS 请求与完成
- 原始音频 binary frame 不会逐帧打印

## Bootstrap 验证

目标接口：

- `POST /v1/bootstrap`

当前 demo seed 常量已经明确，集成后可以先按下面这组值尝试：

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

示例调用：

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

## WebSocket 验证

目标接口：

- `GET /v1/session/ws`

握手时至少要带：

- `Authorization: Bearer <device_token>`
- `X-Device-Id: <device_id>`
- `X-Protocol-Version: v1`

一旦主线程把 handler 接上，如果本地装了 `websocat`，可以先用它做 hello 验证：

```sh
websocat \
  -H="Authorization: Bearer <DEVICE_TOKEN>" \
  -H="X-Device-Id: <DEVICE_ID>" \
  -H="X-Protocol-Version: v1" \
  ws://127.0.0.1:8080/v1/session/ws
```

连接建立后，先发 `hello`。这里要注意，当前协议实现已经确认：

- `hello` 必须带 `request_id`
- `hello` 和 `hello.ok` 都必须用 `payload` 包裹
- `hello.ok` 必须带 `session_id`
- 音频 codec 当前只接受 `pcm_s16le`
- 每个方向同一时间最多只有一个 open stream
- `last_seq` 等于该 stream 已接受 binary frame 的数量

参考消息：

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

hello 通过后，继续验证：

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

对 mock provider 来说，binary frame 的目标主要是验证协议和编排链路，不是音质。只要服务端能稳定产出下列响应，就算这轮 MVP 成功：

- `hello.ok`
- `asr.final`
- 至少一个 `llm.delta`
- `llm.done`
- `tts.start`
- 至少一个下行 binary 音频块
- `tts.stop`
- `action.execute`

## 建议记录的验收证据

当前已经拿到的验证证据：

- `go test ./...` 通过
- `go build ./cmd/agensense` 通过

建议继续记录这些结果，方便后续回归：

- 一次成功的 bootstrap 请求和响应样例
- 一次成功的 WebSocket 会话日志或抓包摘要
- mock 事件的到达顺序

## 故意留到后面的内容

下面这些内容应该等主线程完成集成后，再决定是否补文档：

- 真正的本地 `docker compose`
- `systemd` unit
- k8s manifests
- 真实 provider 凭据配置示例
- 生产环境 token rotation / refresh 说明
