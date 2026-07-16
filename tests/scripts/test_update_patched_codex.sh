#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

origin="$TEST_DIR/origin.git"
seed="$TEST_DIR/seed"
patch_dir="$TEST_DIR/patch-bundle"

git init --bare -b main "$origin" >/dev/null
git init -b main "$seed" >/dev/null
git -C "$seed" config user.name "Patch Test"
git -C "$seed" config user.email "patch-test@example.com"
printf 'upstream fixture\n' >"$seed/README.md"
git -C "$seed" add README.md
git -C "$seed" commit -m "upstream fixture" >/dev/null
git -C "$seed" tag rust-v0.144.4
git -C "$seed" tag rust-v0.145.0-alpha.1
git -C "$seed" tag rust-vrust-v9.0.0
git -C "$seed" remote add origin "$origin"
git -C "$seed" push origin main --tags >/dev/null 2>&1
git --git-dir="$origin" symbolic-ref HEAD refs/heads/main

mkdir -p "$patch_dir"
cat >"$patch_dir/apply-build-install.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

repo=""
for ((index = 1; index <= $#; index++)); do
  if [[ "${!index}" == "--repo" ]]; then
    value_index=$((index + 1))
    repo="${!value_index}"
  fi
done
printf '%s\n' "$@" >"$RUNNER_LOG"
if [[ "${FAKE_CONFLICT:-false}" == true ]]; then
  mkdir -p "$(git -C "$repo" rev-parse --absolute-git-dir)/rebase-apply"
  exit 1
fi
EOF
chmod +x "$patch_dir/apply-build-install.sh"

target_auto="$TEST_DIR/target-auto"
git clone "$origin" "$target_auto" >/dev/null 2>&1
git -C "$target_auto" tag rust-v99.0.0
if ! RUNNER_LOG="$TEST_DIR/auto.log" \
  "$ROOT_DIR/scripts/update-patched-codex.sh" \
  --repo "$target_auto" --patch-dir "$patch_dir" --apply-only \
  >"$TEST_DIR/auto.out" 2>&1; then
  cat "$TEST_DIR/auto.out" >&2
  exit 1
fi

[[ "$(git -C "$target_auto" branch --show-current)" == "patch/token-rate-rust-v0.144.4" ]]
[[ "$(git -C "$target_auto" rev-parse HEAD)" == "$(git -C "$target_auto" rev-parse 'rust-v0.144.4^{commit}')" ]]
grep -Fx -- "--apply-only" "$TEST_DIR/auto.log" >/dev/null

target_explicit="$TEST_DIR/target-explicit"
git clone "$origin" "$target_explicit" >/dev/null 2>&1
if ! RUNNER_LOG="$TEST_DIR/explicit.log" \
  "$ROOT_DIR/scripts/update-patched-codex.sh" rust-v0.145.0-alpha.1 \
  --repo "$target_explicit" --patch-dir "$patch_dir" --apply-only \
  >"$TEST_DIR/explicit.out" 2>&1; then
  cat "$TEST_DIR/explicit.out" >&2
  exit 1
fi

[[ "$(git -C "$target_explicit" branch --show-current)" == \
  "patch/token-rate-rust-v0.145.0-alpha.1" ]]

target_conflict="$TEST_DIR/target-conflict"
git clone "$origin" "$target_conflict" >/dev/null 2>&1
if RUNNER_LOG="$TEST_DIR/conflict.log" FAKE_CONFLICT=true \
  "$ROOT_DIR/scripts/update-patched-codex.sh" rust-v0.144.4 \
  --repo "$target_conflict" --patch-dir "$patch_dir" --apply-only \
  >"$TEST_DIR/conflict.out" 2>&1; then
  printf 'expected simulated patch conflict to stop the update\n' >&2
  exit 1
fi

grep -F "Update paused on patch/token-rate-rust-v0.144.4" "$TEST_DIR/conflict.out" >/dev/null
[[ -d "$(git -C "$target_conflict" rev-parse --absolute-git-dir)/rebase-apply" ]]

printf 'Patched Codex update script tests passed\n'
