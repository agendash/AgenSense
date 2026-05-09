# LocalAI Setup

AgenSense defaults to a local OpenAI-compatible LocalAI server.

Default host runtime address:

```text
http://127.0.0.1:8081/v1
```

Why port `8081`: AgenSense listens on `8080`, while LocalAI examples commonly expose the LocalAI container on its internal `8080` port. Mapping LocalAI to host port `8081` avoids a local port collision.

Official LocalAI docs describe LocalAI as an OpenAI-compatible API server and show Docker startup with port `8080:8080`. They also recommend protecting exposed LocalAI endpoints with API keys or user authentication when not purely local.

## Option 1: Run LocalAI Separately

Start LocalAI on host port `8081`:

```sh
docker run --rm \
  --name local-ai \
  -p 8081:8080 \
  -v "$PWD/tmp/localai-models:/models" \
  localai/localai:latest-cpu
```

Then run AgenSense:

```sh
./scripts/run-local.sh
```

AgenSense will use:

```sh
AGENSENSE_DEFAULT_PROVIDER_BASE_URL=http://127.0.0.1:8081/v1
```

## Option 2: Docker Compose Full Stack

Run AgenSense and LocalAI together:

```sh
./scripts/localai-up.sh
```

This uses:

- `compose.yaml` for AgenSense
- `compose.localai.yaml` for LocalAI

Inside the Docker network, AgenSense reaches LocalAI at:

```text
http://localai:8080/v1
```

From the host, the LocalAI UI/API is exposed at:

```text
http://127.0.0.1:8081
```

## Models

Default AgenSense model names:

- ASR: `whisper-1`
- LLM: `hauhaucs-qwen3.6-35b-a3b-aggressive-q4-k-m`
- Multimodal: inherits the LLM model unless `AGENSENSE_DEFAULT_MULTIMODAL_MODEL` is set
- TTS: `faster-qwen3-tts`

Make sure your LocalAI instance has models with those IDs, or override them:

```sh
export AGENSENSE_DEFAULT_ASR_MODEL="your-asr-model"
export AGENSENSE_DEFAULT_LLM_MODEL="your-llm-model"
export AGENSENSE_DEFAULT_MULTIMODAL_MODEL="your-vision-capable-model"
export AGENSENSE_DEFAULT_TTS_MODEL="your-tts-model"
```

LocalAI can install models from its web UI, its model gallery, or CLI/model URI flows. Model availability depends on the LocalAI installation and hardware.

For the recommended bilingual local TTS path, install LocalAI's `faster-qwen3-tts` gallery model and run AgenSense with:

```sh
export AGENSENSE_DEFAULT_TTS_MODEL="faster-qwen3-tts"
export AGENSENSE_OPENAI_TTS_VOICE="Serena"
export AGENSENSE_OPENAI_TTS_RESPONSE_FORMAT="pcm"
```

`faster-qwen3-tts` handles Chinese and English well in one request. Voice support is backend-specific: the current LocalAI test stack accepts named voices such as `Serena`, while some backends reject generic OpenAI voices such as `alloy`. Set `AGENSENSE_OPENAI_TTS_VOICE=none` if your backend rejects the voice field. AgenSense also unwraps LocalAI WAV responses into `pcm_s16le` frames and reports the actual sample rate returned by the provider.

## API Key

For strictly local development, LocalAI can run without an API key.

If you enable `LOCALAI_API_KEY`, pass the same value to AgenSense:

```sh
export LOCALAI_API_KEY="replace-me"
export AGENSENSE_DEFAULT_PROVIDER_API_KEY="${LOCALAI_API_KEY}"
```

With Compose:

```sh
LOCALAI_API_KEY="replace-me" ./scripts/localai-up.sh
```

## Quick Checks

LocalAI readiness:

```sh
curl -sS http://127.0.0.1:8081/readyz
```

OpenAI-compatible model list:

```sh
curl -sS http://127.0.0.1:8081/v1/models
```

AgenSense health:

```sh
curl -sS http://127.0.0.1:8080/healthz
```

Direct LLM through AgenSense:

```sh
export AGENSENSE_API_KEY="demo-user-key"

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

## References

- LocalAI Quickstart: <https://localai.io/basics/getting_started/>
- LocalAI container images: <https://localai.io/basics/container/>
