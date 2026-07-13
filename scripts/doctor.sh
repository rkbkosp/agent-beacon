#!/usr/bin/env bash

set -euo pipefail

readonly REQUIRED_IDF_VERSION="ESP-IDF v5.5.4"
readonly REQUIRED_GO_VERSION="go1.26.5"
activation_script=${IDF_ACTIVATION_SCRIPT:-"$HOME/.espressif/tools/activate_idf_v5.5.4.sh"}
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
source "$SCRIPT_DIR/lib/idf-tools.sh"

die() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

declare -a IDF_COMMAND=()
if command -v idf.py >/dev/null 2>&1; then
  IDF_COMMAND=(idf.py)
else
  if [[ -z "${IDF_PATH:-}" || -z "${IDF_PYTHON_ENV_PATH:-}" || \
        -z "${IDF_TOOLS_PATH:-}" ]]; then
    [[ -f "$activation_script" ]] || \
      die "ESP-IDF is not active and activation script was not found: $activation_script"
    eim_environment=$(/bin/bash "$activation_script" -e) || \
      die "failed to read ESP-IDF environment from: $activation_script"
    while IFS='=' read -r key value; do
      case "$key" in
        IDF_PATH)
          export IDF_PATH=$value
          ;;
        IDF_PYTHON_ENV_PATH)
          export IDF_PYTHON_ENV_PATH=$value
          ;;
        IDF_TOOLS_PATH)
          export IDF_TOOLS_PATH=$value
          ;;
      esac
    done <<<"$eim_environment"
  fi

  idf_python=${IDF_PYTHON_ENV_PATH:-}/bin/python
  idf_script=${IDF_PATH:-}/tools/idf.py
  [[ -x "$idf_python" && -f "$idf_script" ]] || \
    die "ESP-IDF Python entrypoint is incomplete; rerun the v5.5.4 EIM installation"
  IDF_COMMAND=("$idf_python" "$idf_script")
fi

configure_esptool || die "esptool is unavailable in the ESP-IDF v5.5.4 environment"

for tool in sw_vers uname python3 git go; do
  command -v "$tool" >/dev/null 2>&1 || die "required tool is missing: $tool"
done

printf 'macOS:\n'
sw_vers
printf 'Architecture: %s\n' "$(uname -m)"
python3 --version
git --version
go_version=$(go version)
printf '%s\n' "$go_version"
idf_version=$("${IDF_COMMAND[@]}" --version)
printf '%s\n' "$idf_version"
run_esptool version

[[ "$go_version" == *"$REQUIRED_GO_VERSION"* ]] || \
  die "Go version must be $REQUIRED_GO_VERSION"
[[ "$idf_version" == "$REQUIRED_IDF_VERSION" ]] || \
  die "ESP-IDF version must be v5.5.4"

printf 'doctor: environment matches DEV.md\n'
