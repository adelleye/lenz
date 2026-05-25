#!/usr/bin/env bash

fail() {
  echo "FAIL: $*" >&2
  if [[ -n "${API_LOG:-}" && -f "${API_LOG:-}" ]]; then
    echo "--- API log ---" >&2
    tail -100 "$API_LOG" >&2 || true
  fi
  exit 1
}

pass() {
  echo "PASS: $*"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

compose() {
  POSTGRES_PORT="$POSTGRES_PORT" docker compose -f infra/docker/docker-compose.yml "$@"
}

ensure_docker_running() {
  require_cmd docker
  if docker info >/dev/null 2>&1; then
    return
  fi
  if command -v colima >/dev/null 2>&1; then
    echo "Docker engine is not running; starting Colima..."
    colima start --cpu 2 --memory 4 --disk 20
    return
  fi
  fail "Docker engine is not running. Start Docker Desktop or Colima, then rerun this script."
}

require_free_port() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1 && lsof -tiTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    fail "API port ${port} is already in use. Stop that process or set API_PORT to another port."
  fi
}

wait_container_healthy() {
  local name="$1"
  local status
  for _ in $(seq 1 60); do
    status="$(docker inspect -f '{{.State.Health.Status}}' "$name" 2>/dev/null || true)"
    if [[ "$status" == "healthy" ]]; then
      return 0
    fi
    sleep 1
  done
  fail "container did not become healthy: $name"
}
