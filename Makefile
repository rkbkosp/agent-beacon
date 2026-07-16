SHELL := /bin/bash

PORT ?=
BACKUP ?=
DEMO_DIR ?=
IDF_ACTIVATE ?= $(HOME)/.espressif/tools/activate_idf_v5.5.4.sh
TOKEN_RATE_DAEMON ?= $(HOME)/.local/bin/codex-token-rate-daemon
TOKEN_RATE_DIR ?= $(HOME)/Library/Application Support/AgentBeacon

.PHONY: doctor detect-device backup-factory restore-factory official-demo-build \
	firmware-build firmware-flash firmware-monitor bridge-build bridge-run \
	bridge-install bridge-service-install bridge-service-uninstall bridge-service-restart \
	bridge-service-status token-rate-run token-rate-service-status demo-events test

doctor:
	@IDF_ACTIVATION_SCRIPT="$(IDF_ACTIVATE)" ./scripts/doctor.sh

detect-device:
	@source "$(IDF_ACTIVATE)" >/dev/null; \
	if [[ -n "$(PORT)" ]]; then \
		./scripts/detect-device.sh --port "$(PORT)"; \
	else \
		./scripts/detect-device.sh; \
	fi

backup-factory:
	@test -n "$(PORT)" || (printf 'error: PORT is required\n' >&2; exit 2)
	@source "$(IDF_ACTIVATE)" >/dev/null; \
	./scripts/backup-factory.sh --port "$(PORT)"

restore-factory:
	@test -n "$(PORT)" || (printf 'error: PORT is required\n' >&2; exit 2)
	@test -n "$(BACKUP)" || (printf 'error: BACKUP is required\n' >&2; exit 2)
	@source "$(IDF_ACTIVATE)" >/dev/null; \
	./scripts/restore-factory.sh --port "$(PORT)" --backup "$(BACKUP)"

official-demo-build:
	@test -n "$(DEMO_DIR)" || (printf 'error: DEMO_DIR is required\n' >&2; exit 2)
	@source "$(IDF_ACTIVATE)" >/dev/null; \
	cd "$(DEMO_DIR)"; \
	idf.py build

firmware-build:
	@source "$(IDF_ACTIVATE)" >/dev/null; \
	cd firmware; \
	idf.py build

firmware-flash:
	@test -n "$(PORT)" || (printf 'error: PORT is required\n' >&2; exit 2)
	@source "$(IDF_ACTIVATE)" >/dev/null; \
	./scripts/flash-firmware.sh --port "$(PORT)"

firmware-monitor:
	@test -n "$(PORT)" || (printf 'error: PORT is required\n' >&2; exit 2)
	@source "$(IDF_ACTIVATE)" >/dev/null; \
	./scripts/monitor.sh --port "$(PORT)"

bridge-run:
	@test -s macos/configs/token.local || (printf 'error: run scripts/configure-network.sh first\n' >&2; exit 2)
	@cd macos; AGENT_BEACON_TOKEN="$$(cat configs/token.local)" \
		go run ./cmd/agent-beacon-bridge serve --config configs/config.local.yaml

bridge-build:
	@cd macos; go build -o bin/agent-beacon-bridge ./cmd/agent-beacon-bridge

bridge-install: bridge-build
	@mkdir -p "$(HOME)/.local/bin"
	@cp macos/bin/agent-beacon-bridge "$(HOME)/.local/bin/agent-beacon-bridge"
	@printf 'Installed %s\n' "$(HOME)/.local/bin/agent-beacon-bridge"

bridge-service-install: bridge-build
	@if [[ -n "$${ZERO_API_KEY:-}" ]]; then \
		cd macos; ./bin/agent-beacon-bridge secret set zero-api-key --from-env ZERO_API_KEY; \
	elif ! /usr/bin/security find-generic-password -s com.stepatero.agentbeacon -a zero-api-key >/dev/null 2>&1; then \
		printf 'error: set ZERO_API_KEY for the first install\n' >&2; exit 2; \
	fi
	@cd macos; ./bin/agent-beacon-bridge install-service \
		--config configs/config.local.yaml --token-file configs/token.local

bridge-service-uninstall:
	@"$(HOME)/Library/Application Support/AgentBeacon/bin/agent-beacon-bridge" uninstall-service

bridge-service-restart:
	@/bin/launchctl kickstart -k "gui/$$(id -u)/com.stepatero.agentbeacon"

bridge-service-status:
	@/bin/launchctl print "gui/$$(id -u)/com.stepatero.agentbeacon"

token-rate-run:
	@test -x "$(TOKEN_RATE_DAEMON)" || (printf 'error: patched daemon not found at %s\n' "$(TOKEN_RATE_DAEMON)" >&2; exit 2)
	@mkdir -p "$(TOKEN_RATE_DIR)"
	@"$(TOKEN_RATE_DAEMON)" \
		--socket "$(TOKEN_RATE_DIR)/codex-token-rate.sock" \
		--state-file "$(TOKEN_RATE_DIR)/codex-token-rate.json" --stdout

token-rate-service-status:
	@/bin/launchctl print "gui/$$(id -u)/com.stepatero.agentbeacon.tokenrate"

demo-events:
	@./scripts/emit-demo-events.sh

test:
	@./tests/scripts/test_m0_scripts.sh
	@./tests/scripts/test_configure_network.sh
	@./tests/scripts/test_update_patched_codex.sh
	@./tests/scripts/test_install_fonts.sh
	@./tests/firmware/test_calibration.sh
	@./tests/firmware/test_board_geometry.sh
	@./tests/firmware/test_diagnostics.sh
	@./tests/firmware/test_ui_state.sh
	@./tests/firmware/test_input.sh
	@./tests/firmware/test_ui_model.sh
	@./tests/firmware/test_notifications.sh
	@./tests/firmware/test_protocol.sh
	@./tests/firmware/test_protocol_json.sh
	@./tests/firmware/test_network_policy.sh
	@./tests/firmware/test_network_frame.sh
	@./tests/firmware/test_usb_frame.sh
	@if [[ -f macos/go.mod ]]; then cd macos && go test ./...; fi
