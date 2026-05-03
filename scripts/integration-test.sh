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
    -e API_BASE_URL \
    -e PROMETHEUS_BASE_URL \
    -e GRAFANA_BASE_URL \
    -e POSTGRES_EXPORTER_BASE_URL \
    -e NODE_EXPORTER_BASE_URL \
    -e CADVISOR_BASE_URL \
    -e PROCESSING_BASE_URL \
    -e PROCESSING_HEALTH_PATH \
    -e REQUIRE_EXTERNAL_PROCESSING \
    -v "$PWD":/src \
    -w /src \
    -v bank_go_mod_cache:/go/pkg/mod \
    -v bank_go_build_cache:/root/.cache/go-build \
    "${GO_IMAGE:-golang:1.23}" \
    go "$@"
}

docker compose up -d --build --remove-orphans
run_go test -count=1 -tags=integration ./tests/integration -v
