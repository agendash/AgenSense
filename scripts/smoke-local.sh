#!/usr/bin/env sh
set -eu

export AGENSENSE_API_KEY="${AGENSENSE_API_KEY:-demo-user-key}"
export AGENSENSE_SMOKE_INPUT_SOURCE="${AGENSENSE_SMOKE_INPUT_SOURCE:-tts}"
export AGENSENSE_SMOKE_ENSURE_MOCK_PROVIDER="${AGENSENSE_SMOKE_ENSURE_MOCK_PROVIDER:-true}"

exec go run ./cmd/agensense-smoke "$@"
