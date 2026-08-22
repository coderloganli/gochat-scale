#!/usr/bin/env bash
#
# Measures what a connect instance's departure costs its clients, with graceful
# shutdown on and then off. One build, one procedure, one environment variable
# changed between the arms — the method ADR 0009 used for load shedding.
#
#   make drain-demo
#
# Output is the evidence behind the table in docs/benchmarks.md.
set -euo pipefail

cd "$(dirname "$0")/.."

PROJECT="${DRAIN_PROJECT:-gochat-drain}"
COMPOSE_FILES="docker-compose.yml,deployments/docker-compose.drain.yml"
CONNECTIONS="${DRAIN_CONNECTIONS:-50}"
REPLICAS="${DRAIN_REPLICAS:-2}"
WINDOW="${DRAIN_WINDOW:-20s}"
OUT_DIR="${DRAIN_OUT_DIR:-loadtest/reports/drain}"

compose() {
  docker compose -p "$PROJECT" -f docker-compose.yml -f deployments/docker-compose.drain.yml "$@"
}

cleanup() {
  echo
  echo "tearing the measurement stack down"
  compose down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

mkdir -p "$OUT_DIR"

echo "building the image both arms will run"
compose build >/dev/null

run_arm() {
  arm="$1"
  graceful="$2"

  echo
  echo "=============================================================="
  echo "arm: $arm   (GOCHAT_GRACEFUL_SHUTDOWN=$graceful)"
  echo "=============================================================="

  # A fresh stack per arm, so one arm's restart cannot colour the other's.
  compose down -v --remove-orphans >/dev/null 2>&1 || true
  GOCHAT_GRACEFUL_SHUTDOWN="$graceful" compose up -d --scale "connect-ws=$REPLICAS" >/dev/null

  echo "waiting for the api to answer"
  for _ in $(seq 1 60); do
    if curl -sf -o /dev/null "http://localhost:7070/" 2>/dev/null; then break; fi
    sleep 2
  done
  # Connections need the connect layer registered before they mean anything.
  sleep 10

  go run ./tools/drainbench \
    -arm "$arm" \
    -connections "$CONNECTIONS" \
    -replicas "$REPLICAS" \
    -window "$WINDOW" \
    -project "$PROJECT" \
    -compose-files "$COMPOSE_FILES" \
    -json "$OUT_DIR/$arm.json"
}

run_arm graceful true
run_arm control false

echo
echo "=============================================================="
echo "results written to $OUT_DIR/{graceful,control}.json"
echo "=============================================================="
echo
echo "The two arms ran the same image. What differs between them is what"
echo "graceful shutdown buys, and what it does not: read the etcd residency and"
echo "the close-code split together, and do not expect either arm to deliver more"
echo "messages than the other on the surviving replica."
