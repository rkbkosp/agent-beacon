#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_BEACON_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CODEX_REPO="${CODEX_REPO:-$AGENT_BEACON_ROOT/../codex}"
MODE="full"
RUN_TESTS=true
INSTALL=true
REFRESH_AGENT_BEACON=false

usage() {
  cat <<'EOF'
Usage: apply-build-install.sh [options]

Options:
  --repo PATH              Codex repository (default: ../codex beside agent-bacon)
  --apply-only             Apply the patch series and stop
  --build-only             Skip patch application and build the current tree
  --skip-tests             Skip targeted token-rate tests
  --no-install             Build without installing to ~/.local/bin
  --refresh-agent-beacon   Reinstall/restart the Agent Beacon bridge and daemon
  -h, --help               Show this help

If git am stops on a conflict, resolve it, run git am --continue, then rerun this
script with --build-only.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      [[ $# -ge 2 ]] || { printf 'error: --repo requires a path\n' >&2; exit 2; }
      CODEX_REPO="$2"
      shift 2
      ;;
    --apply-only)
      MODE="apply-only"
      shift
      ;;
    --build-only)
      MODE="build-only"
      shift
      ;;
    --skip-tests)
      RUN_TESTS=false
      shift
      ;;
    --no-install)
      INSTALL=false
      shift
      ;;
    --refresh-agent-beacon)
      REFRESH_AGENT_BEACON=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'error: unknown option: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ "$MODE" == "apply-only" ]] && [[ "$REFRESH_AGENT_BEACON" == true ]]; then
  printf 'error: --apply-only cannot be combined with --refresh-agent-beacon\n' >&2
  exit 2
fi
if [[ "$INSTALL" == false ]] && [[ "$REFRESH_AGENT_BEACON" == true ]]; then
  printf 'error: --no-install cannot be combined with --refresh-agent-beacon\n' >&2
  exit 2
fi
if [[ ! -d "$CODEX_REPO" ]] ||
  ! git -C "$CODEX_REPO" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'error: Codex repository not found: %s\n' "$CODEX_REPO" >&2
  exit 2
fi
CODEX_REPO="$(cd "$CODEX_REPO" && pwd)"

if [[ "$MODE" != "build-only" ]]; then
  if [[ -n "$(git -C "$CODEX_REPO" status --porcelain)" ]]; then
    printf 'error: Codex worktree must be clean before applying patches: %s\n' "$CODEX_REPO" >&2
    exit 2
  fi

  patches=()
  while IFS= read -r patch_name || [[ -n "$patch_name" ]]; do
    [[ -z "$patch_name" ]] && continue
    patch_path="$SCRIPT_DIR/$patch_name"
    if [[ ! -f "$patch_path" ]]; then
      printf 'error: missing patch listed in series: %s\n' "$patch_path" >&2
      exit 2
    fi
    patches+=("$patch_path")
  done < "$SCRIPT_DIR/series"
  if [[ ${#patches[@]} -eq 0 ]]; then
    printf 'error: patch series is empty\n' >&2
    exit 2
  fi

  base_commit="$(<"$SCRIPT_DIR/BASE_COMMIT")"
  if git -C "$CODEX_REPO" cat-file -e "$base_commit^{commit}" 2>/dev/null &&
    ! git -C "$CODEX_REPO" merge-base --is-ancestor "$base_commit" HEAD; then
    printf 'warning: recorded base %s is not an ancestor of HEAD; three-way apply may conflict\n' \
      "$base_commit" >&2
  fi

  if ! git -C "$CODEX_REPO" am --3way "${patches[@]}"; then
    cat >&2 <<'EOF'
Patch application stopped. Resolve conflicts, stage the resolved files, and run:
  git am --continue
Then rerun this script with --build-only. To discard the attempt, run:
  git am --abort
EOF
    exit 1
  fi

  if [[ "$MODE" == "apply-only" ]]; then
    printf 'Applied Codex token-rate patch series at %s\n' "$CODEX_REPO"
    exit 0
  fi
fi

if ! command -v just >/dev/null 2>&1; then
  printf 'error: just is required (macOS: brew install just)\n' >&2
  exit 2
fi

(
  cd "$CODEX_REPO/codex-rs"
  just fmt
  if [[ "$RUN_TESTS" == true ]]; then
    just test -p codex-token-rate-daemon
    just test -p codex-core live_token_rate
  fi
  cargo build --release --bin codex --bin codex-token-rate-daemon
)

if [[ "$INSTALL" == true ]]; then
  "$CODEX_REPO/scripts/install-patched-codex.sh"
fi

if [[ "$REFRESH_AGENT_BEACON" == true ]]; then
  make -C "$AGENT_BEACON_ROOT" bridge-service-install
fi

printf 'Codex token-rate patch build completed at %s\n' "$CODEX_REPO"
if [[ "$INSTALL" == true ]]; then
  printf 'Restart already-running patched Codex processes to load the installed binary.\n'
fi
