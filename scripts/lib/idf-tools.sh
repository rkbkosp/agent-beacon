#!/usr/bin/env bash

declare -a ESPTOOL_COMMAND=()
declare -a IDF_COMMAND=()

configure_idf() {
  if command -v idf.py >/dev/null 2>&1; then
    IDF_COMMAND=(idf.py)
    return 0
  fi

  local python_bin=${IDF_PYTHON_ENV_PATH:-}/bin/python
  local idf_script=${IDF_PATH:-}/tools/idf.py
  if [[ -n "${IDF_PYTHON_ENV_PATH:-}" && -n "${IDF_PATH:-}" && \
        -x "$python_bin" && -f "$idf_script" ]]; then
    IDF_COMMAND=("$python_bin" "$idf_script")
    return 0
  fi

  return 1
}

run_idf() {
  [[ ${#IDF_COMMAND[@]} -gt 0 ]] || configure_idf
  "${IDF_COMMAND[@]}" "$@"
}

configure_esptool() {
  if command -v esptool >/dev/null 2>&1; then
    ESPTOOL_COMMAND=(esptool)
    return 0
  fi

  if command -v esptool.py >/dev/null 2>&1; then
    ESPTOOL_COMMAND=(esptool.py)
    return 0
  fi

  local python_bin=${IDF_PYTHON_ENV_PATH:-}/bin/python
  local esptool_script=${IDF_PATH:-}/components/esptool_py/esptool/esptool.py
  if [[ -n "${IDF_PYTHON_ENV_PATH:-}" && -n "${IDF_PATH:-}" && \
        -x "$python_bin" && -f "$esptool_script" ]]; then
    ESPTOOL_COMMAND=("$python_bin" "$esptool_script")
    return 0
  fi

  return 1
}

run_esptool() {
  [[ ${#ESPTOOL_COMMAND[@]} -gt 0 ]] || configure_esptool
  "${ESPTOOL_COMMAND[@]}" "$@"
}
