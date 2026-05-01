#!/usr/bin/env sh
set -eu

if [ -f .env ]; then
  set -a
  . ./.env
  set +a
fi

docker compose up -d --build --remove-orphans

wait_processing_flag=""
case "${LOAD_WAIT_PROCESSING:-false}" in
  1|true|TRUE|yes|YES)
    wait_processing_flag="-wait-processing"
    ;;
esac

go run ./cmd/loadtest \
  -api "${LOAD_API_BASE_URL:-http://localhost:8000}" \
  -users "${LOAD_USERS:-20}" \
  -payments "${LOAD_PAYMENTS:-200}" \
  -concurrency "${LOAD_CONCURRENCY:-10}" \
  -amount "${LOAD_AMOUNT:-1000}" \
  -timeout "${LOAD_TIMEOUT:-2m}" \
  ${wait_processing_flag}
