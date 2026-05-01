#!/usr/bin/env sh
set -eu

if [ -f .env ]; then
  set -a
  . ./.env
  set +a
fi

docker compose up -d --build --remove-orphans
go test -count=1 -tags=integration ./tests/integration -v
