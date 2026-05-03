#!/usr/bin/env sh
set -eu

if [ -f .env ]; then
  set -a
  . ./.env
  set +a
fi

run_go() {
  if command -v go >/dev/null 2>&1; then
    go "$@"
    return
  fi

  env_file="/dev/null"
  if [ -f .env ]; then
    env_file=".env"
  fi

  docker run --rm --network host \
    --env-file "$env_file" \
    -v "$PWD":/src \
    -w /src \
    -v bank_go_mod_cache:/go/pkg/mod \
    -v bank_go_build_cache:/root/.cache/go-build \
    "${GO_IMAGE:-golang:1.23}" \
    go "$@"
}

docker compose up -d --build --remove-orphans

wait_processing_flag=""
case "${LOAD_WAIT_PROCESSING:-false}" in
  1|true|TRUE|yes|YES)
    wait_processing_flag="-wait-processing"
    ;;
esac

run_go run ./cmd/loadtest \
  -api "${LOAD_API_BASE_URL:-http://localhost:8000}" \
  -users "${LOAD_USERS:-20}" \
  -payments "${LOAD_PAYMENTS:-200}" \
  -concurrency "${LOAD_CONCURRENCY:-10}" \
  -amount "${LOAD_AMOUNT:-1000}" \
  -timeout "${LOAD_TIMEOUT:-2m}" \
  ${wait_processing_flag}
