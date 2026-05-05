#!/usr/bin/env sh
set -eu

docker compose -f compose.yaml -f compose.localai.yaml up --build "$@"
