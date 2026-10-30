#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
backend_dir=$(cd "$script_dir/.." && pwd)
mysql_container=${MOMENTS_MYSQL_CONTAINER:-starmeet-mysql}
api_container=${MOMENTS_API_CONTAINER:-starmeet-api}

# Load host-only publishing secrets before resolving container fallbacks.
moments_env_file=${MOMENTS_ENV_FILE:-"$backend_dir/.moments.env"}
if [[ -f "$moments_env_file" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$moments_env_file"
  set +a
fi

# Daily slots and publish_day follow Taiwan time, independent of the host TZ.
export TZ="${MOMENTS_TZ:-Asia/Taipei}"

# MySQL is intentionally bound only to the compose network in production.
# Resolve its current container IP so host cron jobs do not depend on a
# hard-coded bridge address or an exposed database port.
if [[ -z "${MOMENTS_DB_HOST:-}" ]]; then
  moments_db_host=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$mysql_container")
  if [[ -z "$moments_db_host" ]]; then
    echo "Unable to resolve MySQL container address: $mysql_container" >&2
    exit 1
  fi
  export MOMENTS_DB_HOST="$moments_db_host"
fi

if [[ -z "${MOMENTS_DB_PASS:-}" ]]; then
  moments_db_pass=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$api_container" \
    | sed -n 's/^DATABASE_PASSWORD=//p' | head -n 1)
  if [[ -z "$moments_db_pass" ]]; then
    echo "Unable to read DATABASE_PASSWORD from $api_container" >&2
    exit 1
  fi
  export MOMENTS_DB_PASS="$moments_db_pass"
fi

if [[ -z "${MOMENTS_LLM_API_KEY:-}" ]]; then
  moments_llm_key=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$api_container" \
    | sed -n 's/^MOMENTS_LLM_API_KEY=//p' | head -n 1)
  if [[ -n "$moments_llm_key" ]]; then
    export MOMENTS_LLM_API_KEY="$moments_llm_key"
  fi
fi

# The API service already supports these generic OpenAI-compatible names.
if [[ -z "${MOMENTS_LLM_API_KEY:-}" && -n "${OPENAI_COMPAT_API_KEY:-}" ]]; then
  export MOMENTS_LLM_API_KEY="$OPENAI_COMPAT_API_KEY"
fi
if [[ -z "${MOMENTS_LLM_API_KEY:-}" ]]; then
  moments_openai_key=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$api_container" \
    | sed -n 's/^OPENAI_COMPAT_API_KEY=//p' | head -n 1)
  if [[ -n "$moments_openai_key" ]]; then
    export MOMENTS_LLM_API_KEY="$moments_openai_key"
  fi
fi

# Keep old API container deployments usable while they are being migrated.
if [[ -z "${MOMENTS_LLM_API_KEY:-}" && -z "${DASHSCOPE_API_KEY:-}" ]]; then
  moments_dashscope_key=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$api_container" \
    | sed -n 's/^DASHSCOPE_API_KEY=//p' | head -n 1)
  if [[ -n "$moments_dashscope_key" ]]; then
    export DASHSCOPE_API_KEY="$moments_dashscope_key"
  fi
fi

for required_var in MOMENTS_LLM_API_KEY OSS_ACCESS_KEY_ID OSS_ACCESS_KEY_SECRET PEXELS_API_KEY; do
  # DASHSCOPE_API_KEY is accepted by publish_moments.py as a legacy fallback.
  if [[ "$required_var" == "MOMENTS_LLM_API_KEY" && -n "${DASHSCOPE_API_KEY:-}" ]]; then
    continue
  fi
  if [[ -z "${!required_var:-}" ]]; then
    echo "Missing $required_var in $moments_env_file or the environment" >&2
    exit 1
  fi
done

cd "$backend_dir"
lock_file=${MOMENTS_LOCK_FILE:-/tmp/starmeet-publish-moments.lock}
exec 9>"$lock_file"
if ! flock -n 9; then
  echo "Another publish_moments run is in progress, exiting"
  exit 0
fi
exec python3 "$script_dir/publish_moments.py" "$@"
