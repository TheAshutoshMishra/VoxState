# VoxState — convenience targets wrapping the exact commands documented in
# CLAUDE.md's "Commands" section and README.md's "Setup & running"/
# "Testing" sections. This file adds no new behavior of its own — every
# target is a thin wrapper so you don't have to retype/remember the raw
# go/npm invocations (env var loading, the libopus-gated package list,
# PID/log-file bookkeeping for the background dev targets).

SHELL := /bin/bash

# go isn't always on $PATH in every shell (observed in this dev
# environment) — prefer the canonical install location if present,
# otherwise fall back to whatever `go` resolves to on PATH.
GO := $(shell test -x /usr/local/go/bin/go && echo /usr/local/go/bin/go || echo go)
GOFMT := $(shell test -x /usr/local/go/bin/gofmt && echo /usr/local/go/bin/gofmt || echo gofmt)

BACKEND := backend
FRONTEND := frontend
LOG_DIR := /tmp/voxstate-logs
BACKEND_PID := $(LOG_DIR)/backend.pid
FRONTEND_PID := $(LOG_DIR)/frontend.pid

# Packages that build without libopus/pkg-config (see README's Limitations
# section) — used by targets that should still work if that system
# dependency isn't installed. Once it is, `make backend-test-all` etc.
# below cover every package including internal/voice/livekit.
NO_LIVEKIT_PKGS = $$($(GO) list ./... | grep -v voice/livekit | grep -v cmd/server)

.PHONY: help
help:
	@echo "VoxState — available targets:"
	@echo ""
	@echo "  Setup"
	@echo "    make install              npm install (frontend)"
	@echo ""
	@echo "  Running (foreground — Ctrl-C to stop; use in separate terminals)"
	@echo "    make run-server           real backend (cmd/server) — needs .env + libopus/pkg-config"
	@echo "    make run-devserver        fake-voice backend (cmd/devserver) — no system deps/creds needed"
	@echo "    make run-frontend         frontend dev server"
	@echo ""
	@echo "  Running (background, for one-shot local demo prep)"
	@echo "    make dev                  starts devserver + frontend in the background, logs to $(LOG_DIR)"
	@echo "    make dev-real             starts the real cmd/server (needs .env) + frontend in the background"
	@echo "    make stop                 stops whatever 'make dev'/'make dev-real' started"
	@echo "    make status               checks whether backend (:8080) / frontend (:3000) are reachable"
	@echo "    make logs                 tails both background logs"
	@echo ""
	@echo "  Quality gates"
	@echo "    make fmt                  gofmt -l backend (must print nothing)"
	@echo "    make vet                  go vet, all packages that build without libopus"
	@echo "    make backend-test         go test, all packages that build without libopus"
	@echo "    make backend-test-race    go test -race, same package set"
	@echo "    make backend-test-all     go test ./... (every package — needs libopus/pkg-config)"
	@echo "    make backend-bench        the M9 benchmark suite (policy/tasks/voice)"
	@echo "    make frontend-check       tsc --noEmit + eslint + vitest run"
	@echo "    make frontend-build       next build (production build)"
	@echo "    make check                fmt + vet + backend-test + frontend-check (pre-submission gate)"
	@echo ""
	@echo "  Cleanup"
	@echo "    make clean                removes stray Go binaries (backend/server, backend/devserver) + .next"

# --- Setup -------------------------------------------------------------

.PHONY: install
install:
	cd $(FRONTEND) && npm install

# --- Foreground run targets --------------------------------------------

.PHONY: run-server
run-server:
	@test -f .env || (echo ".env not found — copy .env.example to .env and fill in real values first" && exit 1)
	cd $(BACKEND) && set -a && . ../.env && set +a && $(GO) run ./cmd/server

.PHONY: run-devserver
run-devserver:
	cd $(BACKEND) && $(GO) run ./cmd/devserver

.PHONY: run-frontend
run-frontend:
	cd $(FRONTEND) && npm run dev

# --- Background run targets (for local demo prep) -----------------------

$(LOG_DIR):
	mkdir -p $(LOG_DIR)

.PHONY: dev
dev: $(LOG_DIR)
	@$(MAKE) --no-print-directory stop
	cd $(BACKEND) && nohup $(GO) run ./cmd/devserver > $(LOG_DIR)/backend.log 2>&1 & echo $$! > $(BACKEND_PID)
	cd $(FRONTEND) && nohup npm run dev > $(LOG_DIR)/frontend.log 2>&1 & echo $$! > $(FRONTEND_PID)
	@sleep 2
	@$(MAKE) --no-print-directory status
	@echo "backend uses cmd/devserver (fake LiveKit/Rime/Deepgram) — use 'make dev-real' for real voice"

.PHONY: dev-real
dev-real: $(LOG_DIR)
	@test -f .env || (echo ".env not found — copy .env.example to .env and fill in real values first" && exit 1)
	@$(MAKE) --no-print-directory stop
	cd $(BACKEND) && set -a && . ../.env && set +a && nohup $(GO) run ./cmd/server > $(LOG_DIR)/backend.log 2>&1 & echo $$! > $(BACKEND_PID)
	cd $(FRONTEND) && nohup npm run dev > $(LOG_DIR)/frontend.log 2>&1 & echo $$! > $(FRONTEND_PID)
	@sleep 2
	@$(MAKE) --no-print-directory status

.PHONY: stop
stop:
	@if [ -f $(BACKEND_PID) ]; then kill -9 $$(cat $(BACKEND_PID)) 2>/dev/null; rm -f $(BACKEND_PID); fi
	@pkill -9 -f "cmd/devserver" 2>/dev/null || true
	@pkill -9 -f "cmd/server$$" 2>/dev/null || true
	@if [ -f $(FRONTEND_PID) ]; then kill -9 $$(cat $(FRONTEND_PID)) 2>/dev/null; rm -f $(FRONTEND_PID); fi
	@pkill -9 -f "next-server" 2>/dev/null || true
	@echo "stopped (best-effort — check 'make status' if something still holds the port)"

.PHONY: status
status:
	@curl -s -o /dev/null -w "backend  (:8080): %{http_code}\n" http://localhost:8080/health || echo "backend  (:8080): unreachable"
	@curl -s -o /dev/null -w "frontend (:3000): %{http_code}\n" http://localhost:3000 || echo "frontend (:3000): unreachable"

.PHONY: logs
logs:
	tail -f $(LOG_DIR)/backend.log $(LOG_DIR)/frontend.log

# --- Quality gates -------------------------------------------------------

.PHONY: fmt
fmt:
	cd $(BACKEND) && $(GOFMT) -l .

.PHONY: vet
vet:
	cd $(BACKEND) && $(GO) vet $(NO_LIVEKIT_PKGS)

.PHONY: backend-test
backend-test:
	cd $(BACKEND) && $(GO) test $(NO_LIVEKIT_PKGS)

.PHONY: backend-test-race
backend-test-race:
	cd $(BACKEND) && $(GO) test -race $(NO_LIVEKIT_PKGS)

.PHONY: backend-test-all
backend-test-all:
	cd $(BACKEND) && $(GO) test ./...

.PHONY: backend-bench
backend-bench:
	cd $(BACKEND) && $(GO) test -run '^$$' -bench . -benchtime=200x ./internal/policy/ ./internal/tasks/
	cd $(BACKEND) && $(GO) test -run '^$$' -bench 'BenchmarkInterrupt' -benchtime=100x ./internal/voice/

.PHONY: frontend-check
frontend-check:
	cd $(FRONTEND) && npx tsc --noEmit
	cd $(FRONTEND) && npx eslint .
	cd $(FRONTEND) && npx vitest run

.PHONY: frontend-build
frontend-build:
	cd $(FRONTEND) && npm run build

.PHONY: check
check: fmt vet backend-test frontend-check
	@echo "all quality gates passed"

# --- Cleanup ---------------------------------------------------------

.PHONY: clean
clean:
	rm -f $(BACKEND)/server $(BACKEND)/devserver
	rm -rf $(FRONTEND)/.next
