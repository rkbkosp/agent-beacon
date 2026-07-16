#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_BEACON_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CODEX_REPO="${CODEX_REPO:-$AGENT_BEACON_ROOT/../codex}"
PATCH_DIR="${PATCH_DIR:-$AGENT_BEACON_ROOT/patches/codex-token-rate-rust-v0.144.4}"
TARGET_TAG=""
APPLY_ONLY=false
RUN_TESTS=true
REFRESH_AGENT_BEACON=true

usage() {
  cat <<'EOF'
Usage: update-patched-codex.sh [TAG] [options]

Fetch upstream release tags, create a clean patch branch from the selected tag,
then apply, test, build, and install the Agent Beacon token-rate instrumentation.

Without TAG, the newest stable tag matching rust-vX.Y.Z is selected.

Options:
  --tag TAG                  Explicit upstream release tag
  --repo PATH                Codex repository (default: ../codex)
  --patch-dir PATH           Patch bundle directory
  --apply-only               Stop after the patch series is applied
  --skip-tests               Skip targeted token-rate tests
  --no-refresh-agent-beacon  Do not reinstall/restart Agent Beacon services
  -h, --help                 Show this help

The script never overwrites an existing branch or dirty worktree. If git am
conflicts, it leaves the conflict in place and exits for manual resolution.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag)
      [[ $# -ge 2 ]] || { printf 'error: --tag requires a value\n' >&2; exit 2; }
      [[ -z "$TARGET_TAG" ]] || { printf 'error: tag specified more than once\n' >&2; exit 2; }
      TARGET_TAG="$2"
      shift 2
      ;;
    --repo)
      [[ $# -ge 2 ]] || { printf 'error: --repo requires a path\n' >&2; exit 2; }
      CODEX_REPO="$2"
      shift 2
      ;;
    --patch-dir)
      [[ $# -ge 2 ]] || { printf 'error: --patch-dir requires a path\n' >&2; exit 2; }
      PATCH_DIR="$2"
      shift 2
      ;;
    --apply-only)
      APPLY_ONLY=true
      REFRESH_AGENT_BEACON=false
      shift
      ;;
    --skip-tests)
      RUN_TESTS=false
      shift
      ;;
    --no-refresh-agent-beacon)
      REFRESH_AGENT_BEACON=false
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --*)
      printf 'error: unknown option: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
    *)
      [[ -z "$TARGET_TAG" ]] || { printf 'error: tag specified more than once\n' >&2; exit 2; }
      TARGET_TAG="$1"
      shift
      ;;
  esac
done

if [[ ! -d "$CODEX_REPO" ]] ||
  ! git -C "$CODEX_REPO" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'error: Codex repository not found: %s\n' "$CODEX_REPO" >&2
  exit 2
fi
CODEX_REPO="$(cd "$CODEX_REPO" && pwd)"
requested_patch_dir="$PATCH_DIR"
PATCH_DIR="$(cd "$requested_patch_dir" 2>/dev/null && pwd)" || {
  printf 'error: patch bundle not found: %s\n' "$requested_patch_dir" >&2
  exit 2
}
PATCH_RUNNER="$PATCH_DIR/apply-build-install.sh"
if [[ ! -x "$PATCH_RUNNER" ]]; then
  printf 'error: patch runner is missing or not executable: %s\n' "$PATCH_RUNNER" >&2
  exit 2
fi

git_dir="$(git -C "$CODEX_REPO" rev-parse --absolute-git-dir)"
am_path="$git_dir/rebase-apply"
if [[ -d "$am_path" ]]; then
  cat >&2 <<EOF
error: a git am operation is already in progress at $CODEX_REPO
Resolve it and run git am --continue, or discard it with git am --abort.
EOF
  exit 2
fi
if [[ -n "$(git -C "$CODEX_REPO" status --porcelain)" ]]; then
  printf 'error: Codex worktree must be clean before updating: %s\n' "$CODEX_REPO" >&2
  exit 2
fi

printf 'Fetching release tags from origin...\n'
git -C "$CODEX_REPO" fetch origin --tags --prune
ORIGIN_TAG_REFS="$(git -C "$CODEX_REPO" ls-remote --tags --refs origin 'rust-v*')" || {
  printf 'error: could not list release tags from origin\n' >&2
  exit 2
}

origin_has_tag() {
  local expected_tag="$1"
  local object_id
  local tag_ref

  while IFS=$'\t' read -r object_id tag_ref; do
    if [[ "$tag_ref" == "refs/tags/$expected_tag" ]]; then
      return 0
    fi
  done <<< "$ORIGIN_TAG_REFS"
  return 1
}

if [[ -z "$TARGET_TAG" ]]; then
  while IFS= read -r candidate; do
    if [[ "$candidate" =~ ^rust-v[0-9]+\.[0-9]+\.[0-9]+$ ]] &&
      origin_has_tag "$candidate"; then
      TARGET_TAG="$candidate"
      break
    fi
  done < <(git -C "$CODEX_REPO" tag --list 'rust-v*' --sort=-version:refname)
  if [[ -z "$TARGET_TAG" ]]; then
    printf 'error: no stable rust-vX.Y.Z release tag was found\n' >&2
    exit 2
  fi
fi

if [[ ! "$TARGET_TAG" =~ ^rust-v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  printf 'error: unsupported Codex release tag: %s\n' "$TARGET_TAG" >&2
  exit 2
fi
if ! git -C "$CODEX_REPO" show-ref --verify --quiet "refs/tags/$TARGET_TAG"; then
  printf 'error: tag does not exist after fetching origin: %s\n' "$TARGET_TAG" >&2
  exit 2
fi
if ! origin_has_tag "$TARGET_TAG"; then
  printf 'error: tag does not exist on origin: %s\n' "$TARGET_TAG" >&2
  exit 2
fi

TARGET_COMMIT="$(git -C "$CODEX_REPO" rev-parse "$TARGET_TAG^{commit}")"
PATCH_BRANCH="patch/token-rate-$TARGET_TAG"
if git -C "$CODEX_REPO" show-ref --verify --quiet "refs/heads/$PATCH_BRANCH"; then
  cat >&2 <<EOF
error: local branch already exists: $PATCH_BRANCH
The script will not overwrite it. Inspect or remove that branch explicitly before retrying.
EOF
  exit 2
fi

printf 'Creating %s from %s (%s)...\n' "$PATCH_BRANCH" "$TARGET_TAG" "${TARGET_COMMIT:0:12}"
git -C "$CODEX_REPO" switch -c "$PATCH_BRANCH" "$TARGET_TAG"

runner_args=(--repo "$CODEX_REPO")
if [[ "$APPLY_ONLY" == true ]]; then
  runner_args+=(--apply-only)
else
  if [[ "$RUN_TESTS" == false ]]; then
    runner_args+=(--skip-tests)
  fi
  if [[ "$REFRESH_AGENT_BEACON" == true ]]; then
    runner_args+=(--refresh-agent-beacon)
  fi
fi

if ! "$PATCH_RUNNER" "${runner_args[@]}"; then
  am_path="$git_dir/rebase-apply"
  if [[ -d "$am_path" ]]; then
    cat >&2 <<EOF

Update paused on $PATCH_BRANCH because the patch series conflicts with $TARGET_TAG.
The conflict has been left intact. Resolve it in $CODEX_REPO, then run:
  git add <resolved-files>
  git am --continue
Repeat until git am finishes, then continue with:
  $PATCH_RUNNER --repo $CODEX_REPO --build-only --refresh-agent-beacon
To abandon this update, run:
  git -C $CODEX_REPO am --abort
EOF
  else
    cat >&2 <<EOF

Update stopped after creating $PATCH_BRANCH. Patch application may have completed, but a test,
build, install, or service refresh step failed. Inspect the output above and continue with:
  $PATCH_RUNNER --repo $CODEX_REPO --build-only --refresh-agent-beacon
EOF
  fi
  exit 1
fi

printf '\nPatched Codex update completed.\n'
printf '  tag:    %s\n' "$TARGET_TAG"
printf '  branch: %s\n' "$PATCH_BRANCH"
printf '  head:   %s\n' "$(git -C "$CODEX_REPO" rev-parse HEAD)"
