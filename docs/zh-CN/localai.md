# LocalAI 配置

AgenSense 默认指向本机 OpenAI-compatible LocalAI 服务。

默认地址：

```text
http://127.0.0.1:8081/v1
```

这里使用 `8081` 是为了避免和 AgenSense 自己的 `8080` 端口冲突。LocalAI 容器内部仍然使用 `8080`。

## 单独启动 LocalAI

```sh
docker run --rm \
  --name local-ai \
  -p 8081:8080 \
  -v "$PWD/tmp/localai-models:/models" \
  localai/localai:latest-cpu
```

然后启动 AgenSense：

```sh
./scripts/run-local.sh
```

## Docker Compose 全栈模式

```sh
./scripts/localai-up.sh
```

这会同时使用：

- `compose.yaml`
- `compose.localai.yaml`

容器网络里 AgenSense 访问 LocalAI 的地址是：

```text
http://localai:8080/v1
```

宿主机访问 LocalAI 的地址是：

```text
http://127.0.0.1:8081
```

## 默认模型名

- ASR：`whisper-1`
- LLM：`hauhaucs-qwen3.6-35b-a3b-aggressive-q4-k-m`
- Multimodal：默认继承 LLM 模型，除非显式设置 `AGENSENSE_DEFAULT_MULTIMODAL_MODEL`
- TTS：`faster-qwen3-tts`

如果你的 LocalAI 模型 ID 不同，用环境变量覆盖：

```sh
export AGENSENSE_DEFAULT_ASR_MODEL="your-asr-model"
export AGENSENSE_DEFAULT_LLM_MODEL="your-llm-model"
export AGENSENSE_DEFAULT_MULTIMODAL_MODEL="your-vision-capable-model"
export AGENSENSE_DEFAULT_TTS_MODEL="your-tts-model"
```

推荐的本地双语 TTS 路径是 LocalAI gallery 里的 `faster-qwen3-tts`：

```sh
export AGENSENSE_DEFAULT_TTS_MODEL="faster-qwen3-tts"
export AGENSENSE_OPENAI_TTS_VOICE="Serena"
export AGENSENSE_OPENAI_TTS_RESPONSE_FORMAT="pcm"
```

`faster-qwen3-tts` 的中英文稳定性更适合作为默认验证链路。voice 支持取决于具体后端：当前 LocalAI 测试栈可以接受 `Serena` 这类命名声音，但有些后端会拒绝 `alloy` 这类通用 OpenAI voice 名称。如果后端不接受 voice 字段，可以设置 `AGENSENSE_OPENAI_TTS_VOICE=none`。AgenSense 会把 LocalAI 返回的 16-bit WAV 自动拆成 `pcm_s16le`，并把真实采样率回传给客户端。

## API Key

纯本地开发可以不设置 LocalAI API key。

如果启用了 `LOCALAI_API_KEY`，把同一个值传给 AgenSense：

```sh
export LOCALAI_API_KEY="replace-me"
export AGENSENSE_DEFAULT_PROVIDER_API_KEY="${LOCALAI_API_KEY}"
```

Compose：

```sh
LOCALAI_API_KEY="replace-me" ./scripts/localai-up.sh
```

## 快速检查

```sh
curl -sS http://127.0.0.1:8081/readyz
curl -sS http://127.0.0.1:8081/v1/models
curl -sS http://127.0.0.1:8080/healthz
```

参考：

- <https://localai.io/basics/getting_started/>
- <https://localai.io/basics/container/>
