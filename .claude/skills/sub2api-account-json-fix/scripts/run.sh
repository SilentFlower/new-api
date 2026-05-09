#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
CLAUDE_SKILL_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SKILL_GARDEN_ROOT="$(cd "$CLAUDE_SKILL_DIR/../../.." && pwd -P)"
MAIN_SCRIPT="$SKILL_GARDEN_ROOT/.codex/skills/sub2api-account-json-fix/scripts/fix_exported_account_json.py"
LOCAL_ENV_FILE="$CLAUDE_SKILL_DIR/env/push.env"

export SUB2API_ACCOUNT_JSON_FIX_ENV_FILE="$LOCAL_ENV_FILE"

if [[ -f "$LOCAL_ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$LOCAL_ENV_FILE"
  set +a
fi

if [[ $# -lt 1 ]]; then
  echo "用法: run.sh <template-json> [target-json...]" >&2
  exit 2
fi

TEMPLATE_FILE="$1"
shift

python "$MAIN_SCRIPT" --template "$TEMPLATE_FILE" "$@" --push
