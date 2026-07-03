#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="$ROOT_DIR/deploy/.env"
COMPOSE_FILE="$ROOT_DIR/deploy/docker-compose.yml"
CURRENT_STEP="initializing"
INFRA_SERVICES=(postgres redis qdrant minio)

on_exit() {
  status=$?
  if (( status == 0 )); then
    echo "local dev-up: completed successfully"
  else
    echo "local dev-up: failed during ${CURRENT_STEP} (exit ${status})" >&2
    case "$CURRENT_STEP" in
      "checking local tool dependencies")
        echo "Install the missing host tool(s), then rerun ./scripts/local/dev-up.sh." >&2
        ;;
      "initializing MinIO buckets")
        echo "Check MinIO initialization logs with: docker compose -f deploy/docker-compose.yml --env-file deploy/.env logs minio-init" >&2
        echo "Check Docker with: docker compose -f deploy/docker-compose.yml --env-file deploy/.env ps" >&2
        ;;
      *)
        echo "Check Docker with: docker compose -f deploy/docker-compose.yml --env-file deploy/.env ps" >&2
        echo "If Go module download failed, confirm deploy/.env contains GOPROXY and GOSUMDB." >&2
        ;;
    esac
  fi
}
trap on_exit EXIT

run_step() {
  CURRENT_STEP="$1"
  shift
  echo "local dev-up: ${CURRENT_STEP}"
  "$@"
  echo "local dev-up: ${CURRENT_STEP} succeeded"
}

check_required_commands() {
  local missing=()
  for command in docker go psql; do
    if ! command -v "$command" >/dev/null 2>&1; then
      missing+=("$command")
    fi
  done
  if [[ -n "${QDRANT_URL:-}" ]] && ! command -v curl >/dev/null 2>&1; then
    missing+=(curl)
  fi

  if (( ${#missing[@]} > 0 )); then
    echo "missing required local command(s): ${missing[*]}" >&2
    echo "Install Docker, Go, psql, and curl in the same host environment that runs ./scripts/local/dev-up.sh." >&2
    return 1
  fi
}

run_minio_init() {
  CURRENT_STEP="initializing MinIO buckets"
  echo "local dev-up: ${CURRENT_STEP}"
  if ! "${compose[@]}" up --no-deps --exit-code-from minio-init minio-init; then
    echo "minio-init failed; inspect logs with: docker compose -f deploy/docker-compose.yml --env-file deploy/.env logs minio-init" >&2
    return 1
  fi
  echo "local dev-up: ${CURRENT_STEP} succeeded"
}

ensure_go_module_settings() {
  if ! command -v go >/dev/null 2>&1; then
    echo "go is required for host-run migrations" >&2
    return 1
  fi

  effective_goproxy="${GOPROXY:-$(go env GOPROXY 2>/dev/null || true)}"
  effective_gosumdb="${GOSUMDB:-$(go env GOSUMDB 2>/dev/null || true)}"

  if [[ -z "${GOPROXY:-}" && ( -z "$effective_goproxy" || "$effective_goproxy" == *"proxy.golang.org"* ) ]]; then
    export GOPROXY="https://goproxy.cn,direct"
    echo "deploy/.env did not set GOPROXY; using repository default for this run: $GOPROXY"
  elif [[ -z "${GOPROXY:-}" ]]; then
    export GOPROXY="$effective_goproxy"
    echo "deploy/.env did not set GOPROXY; using global go env value: $GOPROXY"
  fi

  if [[ -z "${GOSUMDB:-}" && ( -z "$effective_gosumdb" || "$effective_gosumdb" == "sum.golang.org" ) ]]; then
    export GOSUMDB="sum.golang.google.cn"
    echo "deploy/.env did not set GOSUMDB; using repository default for this run: $GOSUMDB"
  elif [[ -z "${GOSUMDB:-}" ]]; then
    export GOSUMDB="$effective_gosumdb"
    echo "deploy/.env did not set GOSUMDB; using global go env value: $GOSUMDB"
  fi

  if [[ "$GOPROXY" == *"proxy.golang.org"* ]]; then
    echo "warning: GOPROXY includes proxy.golang.org; this may time out on some developer networks" >&2
    echo "current GOPROXY=$GOPROXY" >&2
  fi
}

migrate_service() {
  service="$1"
  database_url="$2"
  CURRENT_STEP="migrating $service"
  echo "local dev-up: ${CURRENT_STEP}"
  (
    cd "$ROOT_DIR/services/$service"
    go run github.com/pressly/goose/v3/cmd/goose@v3.27.1 -dir migrations postgres "$database_url" up
  )
  echo "local dev-up: ${CURRENT_STEP} succeeded"
}

echo "local dev-up: starting infra, migrations, and seed"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing deploy/.env; run: cp deploy/.env.example deploy/.env" >&2
  exit 1
fi

# deploy/.env is copied by the user from deploy/.env.example. The script does
# not own defaults; it only exposes that file to host migration/seed commands.
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

compose=(docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE")

initialize_qdrant_collection() {
  CURRENT_STEP="initializing Qdrant collection"
  qdrant_url="${QDRANT_URL:-}"

  if [[ -z "$qdrant_url" ]]; then
    echo "QDRANT_URL is empty; skipping Qdrant collection initialization"
    return
  fi
  qdrant_collection="${QDRANT_COLLECTION:?QDRANT_COLLECTION must be set in deploy/.env}"
  embedding_dimension="${EMBEDDING_DIMENSION:?EMBEDDING_DIMENSION must be set in deploy/.env}"
  if [[ ! "$qdrant_collection" =~ ^[A-Za-z0-9_.-]+$ ]]; then
    echo "QDRANT_COLLECTION must contain only letters, numbers, dots, underscores, or hyphens" >&2
    return 1
  fi
  if [[ ! "$embedding_dimension" =~ ^[1-9][0-9]*$ ]]; then
    echo "EMBEDDING_DIMENSION must be a positive integer" >&2
    return 1
  fi

  qdrant_url="${qdrant_url%/}"
  response_file="$(mktemp)"
  status="$(
    curl --noproxy '*' -sS -o "$response_file" -w '%{http_code}' \
      "$qdrant_url/collections/$qdrant_collection" || true
  )"

  case "$status" in
    200)
      compact_response="$(tr -d '[:space:]' <"$response_file")"
      rm -f "$response_file"
      if [[ "$compact_response" != *"\"vectors\":{\"size\":$embedding_dimension"* ]] ||
        [[ "$compact_response" != *"\"distance\":\"Cosine\""* ]]; then
        echo "Qdrant collection $qdrant_collection exists but does not match EMBEDDING_DIMENSION=$embedding_dimension" >&2
        return 1
      fi
      echo "Qdrant collection $qdrant_collection is ready"
      ;;
    404)
      rm -f "$response_file"
      echo "creating Qdrant collection $qdrant_collection"
      curl --noproxy '*' -fsS -X PUT "$qdrant_url/collections/$qdrant_collection" \
        -H 'Content-Type: application/json' \
        --data "{\"vectors\":{\"size\":$embedding_dimension,\"distance\":\"Cosine\"}}" >/dev/null
      ;;
    *)
      echo "could not inspect Qdrant collection $qdrant_collection at $qdrant_url (HTTP $status)" >&2
      rm -f "$response_file"
      return 1
      ;;
  esac
}

run_step "checking local tool dependencies" check_required_commands
run_step "validating Docker Compose config" "${compose[@]}" config --quiet
run_step "pulling infrastructure images" "${compose[@]}" pull
run_step "starting infrastructure and waiting for health" "${compose[@]}" up -d --wait --wait-timeout "${LOCAL_INFRA_WAIT_TIMEOUT_SECONDS:-180}" "${INFRA_SERVICES[@]}"
run_minio_init

initialize_qdrant_collection
run_step "checking Go module settings" ensure_go_module_settings

for item in \
  "auth:$AUTH_DATABASE_URL" \
  "file:$FILE_DATABASE_URL" \
  "knowledge:$KNOWLEDGE_DATABASE_URL" \
  "qa:$QA_DATABASE_URL" \
  "document:$DOCUMENT_DATABASE_URL" \
  "ai-gateway:$AI_GATEWAY_DATABASE_URL"; do
  service="${item%%:*}"
  database_url="${item#*:}"
  migrate_service "$service" "$database_url"
done

CURRENT_STEP="applying local demo seed"
echo "local dev-up: ${CURRENT_STEP}"
psql "$POSTGRES_ADMIN_URL" \
  -v ON_ERROR_STOP=1 \
  -f "$ROOT_DIR/deploy/seeds/001-local-demo-seed.sql" \
  -f "$ROOT_DIR/deploy/seeds/002-ai-gateway-model-profiles.sql" \
  -f "$ROOT_DIR/deploy/seeds/003-qa-document-mcp.sql" \
  -f "$ROOT_DIR/deploy/seeds/004-qa-default-knowledge-base.sql"
echo "local dev-up: ${CURRENT_STEP} succeeded"

echo "infra, migrations, and seed are ready"
