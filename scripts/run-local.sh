#!/usr/bin/env sh
set -eu

export AGENSENSE_ADDR="${AGENSENSE_ADDR:-:8080}"
export AGENSENSE_PUBLIC_BASE_URL="${AGENSENSE_PUBLIC_BASE_URL:-http://127.0.0.1:8080}"
export AGENSENSE_DATA_DIR="${AGENSENSE_DATA_DIR:-tmp/agensense}"
export AGENSENSE_LOG_LEVEL="${AGENSENSE_LOG_LEVEL:-info}"

exec go run ./cmd/agensense
