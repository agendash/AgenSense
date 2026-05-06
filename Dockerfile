# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/agensense ./cmd/agensense

FROM alpine:3.22

RUN addgroup -S agensense && adduser -S -G agensense agensense

COPY --from=build /out/agensense /usr/local/bin/agensense

RUN mkdir -p /data && chown -R agensense:agensense /data

USER agensense

ENV AGENSENSE_ADDR=:8080
ENV AGENSENSE_PUBLIC_BASE_URL=http://127.0.0.1:8080
ENV AGENSENSE_DATA_DIR=/data
ENV AGENSENSE_LOG_LEVEL=info
ENV AGENSENSE_DEFAULT_PROVIDER_BASE_URL=http://host.docker.internal:8081/v1
ENV AGENSENSE_DEFAULT_ASR_MODEL=whisper-1
ENV AGENSENSE_DEFAULT_LLM_MODEL=gemma-4-e2b-it
ENV AGENSENSE_DEFAULT_TTS_MODEL=faster-qwen3-tts
ENV AGENSENSE_OPENAI_TTS_VOICE=none
ENV AGENSENSE_OPENAI_TTS_RESPONSE_FORMAT=pcm

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["agensense"]
