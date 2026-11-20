#!/usr/bin/env bash
# dev-run.sh — Levanta el dashboard localmente para pruebas rápidas.
# No requiere Docker ni rebuild de imagen. Los cambios en Go requieren
# volver a ejecutar este script; los cambios en templates/CSS/JS se
# reflejan al recargar (están embebidos en el binario compilado por go run).
#
# Uso:
#   chmod +x dev-run.sh
#   ./dev-run.sh           # puerto 3001, sin Docker, sin IP pública
#   PORT=8080 ./dev-run.sh # puerto personalizado

set -euo pipefail

PORT="${PORT:-3001}"
LOG_LEVEL="${LOG_LEVEL:-debug}"
POLLING_INTERVAL="${POLLING_INTERVAL:-5s}"
COMPOSE_SCAN_PATH="${COMPOSE_SCAN_PATH:-}"
ENABLE_PUBLIC_IP="${ENABLE_PUBLIC_IP:-false}"

echo ""
echo "┌─────────────────────────────────────────────────┐"
echo "│  Home Lab Dashboard — modo desarrollo local      │"
echo "│  http://localhost:${PORT}                            │"
echo "│  Ctrl+C para detener                             │"
echo "└─────────────────────────────────────────────────┘"
echo ""
echo "  PORT=$PORT  POLLING=$POLLING_INTERVAL  LOG=$LOG_LEVEL"
echo "  DOCKER: deshabilitado (DOCKER_HOST vacío)"
echo "  PUBLIC_IP: $ENABLE_PUBLIC_IP"
echo ""

PORT="$PORT" \
LOG_LEVEL="$LOG_LEVEL" \
POLLING_INTERVAL="$POLLING_INTERVAL" \
COMPOSE_SCAN_PATH="$COMPOSE_SCAN_PATH" \
ENABLE_PUBLIC_IP="$ENABLE_PUBLIC_IP" \
GOCACHE=/tmp/hld-go-cache \
  go run .
