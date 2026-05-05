# Deployment

This document covers the deployment paths currently supported by the repository.

## Local Binary

Build and run:

```sh
go build -o bin/agensense ./cmd/agensense
AGENSENSE_ADDR=:8080 \
AGENSENSE_PUBLIC_BASE_URL=http://127.0.0.1:8080 \
AGENSENSE_DATA_DIR=tmp/agensense \
AGENSENSE_DEFAULT_PROVIDER_BASE_URL=http://127.0.0.1:8081/v1 \
./bin/agensense
```

Or use:

```sh
./scripts/run-local.sh
```

## Docker Compose

Start AgenSense only. This expects LocalAI to be reachable from the container through `host.docker.internal:8081`.

```sh
docker compose up --build
```

Health check:

```sh
curl -sS http://127.0.0.1:8080/healthz
```

Stop:

```sh
docker compose down
```

Remove the local data volume:

```sh
docker compose down -v
```

## Docker Compose With LocalAI

Start AgenSense and LocalAI together:

```sh
docker compose -f compose.yaml -f compose.localai.yaml up --build
```

Or:

```sh
./scripts/localai-up.sh
```

When running inside Compose, AgenSense uses `http://localai:8080/v1`. The LocalAI service is exposed to the host at `http://127.0.0.1:8081`.

See [LocalAI setup](localai.md) for model and API-key details.

## Container Image

Build:

```sh
docker build -t agendash/agensense:local .
```

Run:

```sh
docker run --rm \
  -p 8080:8080 \
  -e AGENSENSE_ADDR=:8080 \
  -e AGENSENSE_PUBLIC_BASE_URL=http://127.0.0.1:8080 \
  -e AGENSENSE_DATA_DIR=/data \
  -e AGENSENSE_DEFAULT_PROVIDER_BASE_URL=http://host.docker.internal:8081/v1 \
  --add-host host.docker.internal:host-gateway \
  -v agensense-data:/data \
  agendash/agensense:local
```

## Scripts

The scripts under `scripts/` are intentionally small wrappers:

- `scripts/run-local.sh`: run the service with local defaults
- `scripts/smoke-local.sh`: run the voice smoke test against a local service
- `scripts/docker-local.sh`: run `docker compose up --build`
- `scripts/localai-up.sh`: run AgenSense and LocalAI together

They are source-controlled because they document the expected local workflow and reduce command drift.

## Environment Variables

Common runtime variables:

- `AGENSENSE_ADDR`
- `AGENSENSE_PUBLIC_BASE_URL`
- `AGENSENSE_DATA_DIR`
- `AGENSENSE_LOG_LEVEL`
- `AGENSENSE_DEBUG`
- `AGENSENSE_DISABLE_DEMO_SEED`
- `AGENSENSE_DEFAULT_API_KEY`
- `AGENSENSE_DEFAULT_PROVIDER_BASE_URL`
- `AGENSENSE_DEFAULT_PROVIDER_API_KEY`
- `AGENSENSE_DEFAULT_ASR_MODEL`
- `AGENSENSE_DEFAULT_LLM_MODEL`
- `AGENSENSE_DEFAULT_TTS_MODEL`

## State And Secrets

The current runtime stores provider profiles in the local JSON store. Provider API keys are stored in that file as plain text.

For production, add a real secret-management layer before allowing untrusted users to register provider credentials.

## Production Gaps

The checked-in Docker Compose file is a local deployment example. It is not a complete production stack.

Missing production pieces:

- external durable database
- credential encryption
- audit logging
- metrics and tracing pipeline
- rate limits and quotas
- TLS termination
- backup and restore process
