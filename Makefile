GOCACHE ?= $(CURDIR)/.gocache
BIN_DIR ?= $(CURDIR)/bin
APP      = miniform
TAILWIND = $(BIN_DIR)/tailwindcss
GO_IMAGE ?= golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651
ALPINE_IMAGE ?= alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40
INSTALLER_BINARY ?= $(BIN_DIR)/miniform-linux
INSTALLER_BINARY_DIR = $(abspath $(dir $(INSTALLER_BINARY)))
INSTALLER_BINARY_NAME = $(notdir $(INSTALLER_BINARY))
E2E_BINARY ?= $(BIN_DIR)/miniform-e2e
APPLE_CONTAINER_IMAGE ?= miniform:local
APPLE_CONTAINER_NAME ?= miniform
APPLE_CONTAINER_PORT ?= 8080
APPLE_CONTAINER_STORAGE ?= $(CURDIR)/storage
DEMO_DATA_DIR ?= $(CURDIR)/tmp/demo
DEMO_PORT ?= 8080

WATCHEXEC ?= $(shell command -v watchexec 2>/dev/null)
GOTESTSUM ?= $(shell command -v gotestsum 2>/dev/null)
TOOLS_DIR ?= $(CURDIR)/.tools
GOLANGCI_LINT_VERSION ?= v2.12.2
GOVULNCHECK_VERSION ?= v1.6.0
GOSEC_VERSION ?= v2.28.0
GITLEAKS_VERSION ?= v8.30.1
ACTIONLINT_VERSION ?= v1.7.12
DEADCODE_VERSION ?= v0.48.0
SHELLCHECK_VERSION ?= v0.11.0
GOLANGCI_LINT_STAMP = $(TOOLS_DIR)/.golangci-lint-$(GOLANGCI_LINT_VERSION)
DEADCODE_STAMP = $(TOOLS_DIR)/.deadcode-$(DEADCODE_VERSION)
SHELLCHECK_STAMP = $(TOOLS_DIR)/.shellcheck-$(SHELLCHECK_VERSION)

.PHONY: help bootstrap build installer-binary run seed dev demo test test-unit test-race test-e2e test-e2e-setup test-integration test-release-binaries tidy fmt fmt-check lint shell-lint workflow-lint check audit audit-secrets audit-licenses licenses audit-node audit-security verify-generated clean deps release release-check vendor css css-watch container-build container-test apple-container-start apple-container-build apple-container-run apple-container-health apple-container-stop

help:
	@echo "Available targets:"
	@echo "  bootstrap    - install pinned frontend and E2E dependencies"
	@echo "  deps         - create local build and tool directories"
	@echo "  vendor       - download JS/CSS dependencies (htmx, highlight.js, tailwind)"
	@echo "  css          - build Tailwind CSS for production"
	@echo "  css-watch    - watch and rebuild Tailwind CSS on changes"
	@echo "  build        - compile the CLI binary to $(BIN_DIR)"
	@echo "  run          - run the application from source"
	@echo "  dev          - hot-reload the server using watchexec (requires .env)"
	@echo "  demo         - seed and run an isolated local instance with a test form"
	@echo "  test         - run unit & e2e tests"
	@echo "  test-unit    - run all non-VM Go tests"
	@echo "  test-race    - run Go tests with the race detector"
	@echo "  test-e2e     - run Playwright end-to-end tests in e2e/"
	@echo "  test-e2e-setup - install Playwright dependencies"
	@echo "  test-integration - run VM-based installer integration tests (requires OrbStack)"
	@echo "  test-release-binaries - run GoReleaser binaries on pinned Alpine"
	@echo "  check        - run formatting, modules, generated files, lint, and Go tests"
	@echo "  workflow-lint - validate GitHub Actions workflows"
	@echo "  shell-lint   - analyze shell scripts with ShellCheck"
	@echo "  audit        - scan secrets, licenses, Go/Node vulnerabilities, and Go security"
	@echo "  licenses     - refresh bundled Go dependency license texts"
	@echo "  tidy         - add/remove go.mod entries"
	@echo "  fmt          - gofmt Go source files"
	@echo "  clean        - remove build artifacts"
	@echo "  container-test - build the OCI image and verify its health with Docker"
	@echo "  apple-container-run - build and run Miniform with Apple container"
	@echo "  apple-container-health - verify the running Apple container"
	@echo "  apple-container-stop - stop the Apple container instance"
	@echo "  release      - tag & push to trigger the GoReleaser pipeline (make release v=X.Y.Z)"

deps:
	@mkdir -p $(GOCACHE) $(BIN_DIR) $(TOOLS_DIR)

bootstrap: vendor css test-e2e-setup

vendor:
	@./scripts/vendor.sh

css: vendor
	@echo ">> building Tailwind CSS"
	$(TAILWIND) -i web/static/app.css -o web/static/app.built.css --minify

css-watch: vendor
	@echo ">> watching Tailwind CSS"
	$(TAILWIND) -i web/static/app.css -o web/static/app.built.css --watch

COMMIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
VERSION ?= dev

build: deps
	@echo ">> building $(APP)"
	GOCACHE=$(GOCACHE) go build -trimpath \
		-ldflags="-X main.version=$(VERSION) -X main.commit=$(COMMIT_SHA) -X github.com/matteodante/miniform/internal/server.buildCommit=$(COMMIT_SHA)" \
		-o $(BIN_DIR)/$(APP) ./cmd/$(APP)

installer-binary: deps
	@echo ">> building CGO-enabled Linux installer binary"
	@mkdir -p "$(INSTALLER_BINARY_DIR)"
	docker run --rm \
		--user "$$(id -u):$$(id -g)" \
		--volume "$(CURDIR):/src:ro" \
		--volume "$(INSTALLER_BINARY_DIR):/out" \
		--workdir /src \
		--env CGO_ENABLED=1 \
		--env GOCACHE=/tmp/go-build \
		--env GOMODCACHE=/tmp/go-mod \
		--env HOME=/tmp \
		$(GO_IMAGE) \
		go build -trimpath -o /out/$(INSTALLER_BINARY_NAME) ./cmd/$(APP)

run: deps
	MINIFORM_ENV=development GOCACHE=$(GOCACHE) go run ./cmd/$(APP)

seed: deps
	@echo ">> seeding database"
	MINIFORM_ENV=development GOCACHE=$(GOCACHE) go run ./cmd/$(APP) --seed

dev: deps
ifeq ($(strip $(WATCHEXEC)),)
	@echo "watchexec not found. Install via 'brew install watchexec' or see https://github.com/watchexec/watchexec"
	@exit 1
else
	MINIFORM_ENV=development GOCACHE=$(GOCACHE) $(WATCHEXEC) --clear --restart \
		--watch cmd --watch internal --watch web \
		--exts go,html,tmpl \
		-- go run ./cmd/$(APP)
endif

demo: deps
	@echo ">> preparing isolated demo data in $(DEMO_DATA_DIR)"
	MINIFORM_ENV=test MINIFORM_PORT=$(DEMO_PORT) MINIFORM_DATA_DIR=$(DEMO_DATA_DIR) GOCACHE=$(GOCACHE) go run ./cmd/$(APP) --seed
	@echo ">> test page: http://127.0.0.1:$(DEMO_PORT)/_demo"
	@echo ">> admin: admin@miniform.local / miniform"
	MINIFORM_ENV=test MINIFORM_PORT=$(DEMO_PORT) MINIFORM_DATA_DIR=$(DEMO_DATA_DIR) GOCACHE=$(GOCACHE) go run ./cmd/$(APP)

test: test-unit test-e2e

test-unit: deps
ifeq ($(strip $(GOTESTSUM)),)
	@echo ">> go test ./..."
	MINIFORM_ENV=test GOCACHE=$(GOCACHE) go test -count=1 ./...
else
	@echo ">> gotestsum ./..."
	MINIFORM_ENV=test GOCACHE=$(GOCACHE) $(GOTESTSUM) --format testname -- -count=1 ./...
endif

test-race: deps
	@echo ">> go test -race ./..."
	MINIFORM_ENV=test GOCACHE=$(GOCACHE) go test -race -count=1 ./...

test-e2e-setup:
	@echo ">> installing Playwright dependencies"
	cd e2e && npm ci && npx playwright install chromium

test-e2e: deps
	@echo ">> building E2E server"
	MINIFORM_ENV=test GOCACHE=$(GOCACHE) go build -trimpath -o $(E2E_BINARY) ./cmd/$(APP)
	@echo ">> running Node and Playwright E2E tests"
	cd e2e && npm run test:unit
	cd e2e && MINIFORM_E2E_SERVER_COMMAND="$(E2E_BINARY)" npm test

test-integration: installer-binary
	@echo ">> running VM-based installer integration tests"
	@echo "   (requires OrbStack and the orb CLI)"
	BINARY_PATH=$(INSTALLER_BINARY) MINIFORM_ENV=test MINIFORM_RUN_INSTALLATION_TEST=1 go test -v -timeout 15m ./tests/integration/...

test-release-binaries:
	@./scripts/test-release-binaries.sh dist/artifacts.json "$(ALPINE_IMAGE)"

tidy: deps
	GOCACHE=$(GOCACHE) go mod tidy

fmt:
	@echo ">> formatting Go files"
	go fmt ./...

fmt-check:
	@echo ">> checking Go formatting"
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.gocache/*' -not -path './.tools/*'))" || \
		(echo "Go files need formatting; run 'make fmt'"; gofmt -l $$(find . -name '*.go' -not -path './.gocache/*' -not -path './.tools/*'); exit 1)

$(GOLANGCI_LINT_STAMP): scripts/install-golangci-lint.sh | deps
	@./scripts/install-golangci-lint.sh "$(GOLANGCI_LINT_VERSION)" "$(TOOLS_DIR)"
	@test "$$($(TOOLS_DIR)/golangci-lint version --short)" = "$(patsubst v%,%,$(GOLANGCI_LINT_VERSION))"
	@touch "$@"

$(DEADCODE_STAMP): | deps
	GOBIN=$(TOOLS_DIR) go install golang.org/x/tools/cmd/deadcode@$(DEADCODE_VERSION)
	@touch "$@"

lint: $(GOLANGCI_LINT_STAMP) $(DEADCODE_STAMP)
	@echo ">> verifying golangci-lint configuration"
	@$(TOOLS_DIR)/golangci-lint config verify
	@echo ">> running golangci-lint"
	GOCACHE=$(GOCACHE) $(TOOLS_DIR)/golangci-lint run
	@echo ">> finding unreachable Go functions"
	@set -e; output="$$(GOCACHE=$(GOCACHE) $(TOOLS_DIR)/deadcode -test ./...)"; \
		test -z "$$output" || (printf '%s\n' "$$output"; exit 1)

$(SHELLCHECK_STAMP): scripts/install-shellcheck.sh | deps
	@./scripts/install-shellcheck.sh "$(SHELLCHECK_VERSION)" "$(TOOLS_DIR)"
	@touch "$@"

shell-lint: $(SHELLCHECK_STAMP)
	@echo ">> analyzing shell scripts"
	@$(TOOLS_DIR)/shellcheck -x docker-entrypoint.sh install.sh scripts/*.sh

workflow-lint: $(SHELLCHECK_STAMP) | deps
	@echo ">> validating GitHub Actions workflows"
	@./scripts/ci-policy_test.sh
	@GOBIN=$(TOOLS_DIR) go install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)
	@$(TOOLS_DIR)/actionlint -shellcheck=$(TOOLS_DIR)/shellcheck .github/workflows/*.yml

verify-generated: vendor
	@echo ">> checking generated and vendored assets"
	@temporary=$$(mktemp); \
		trap 'rm "$$temporary"' EXIT; \
		$(TAILWIND) -i web/static/app.css -o "$$temporary" --minify; \
		cmp -s web/static/app.built.css "$$temporary" || \
		(echo "web/static/app.built.css is stale; run 'make css'"; diff -u web/static/app.built.css "$$temporary"; exit 1)
	@git diff --exit-code -- web/static/vendor

check: fmt-check
	@echo ">> verifying Go modules"
	@go mod tidy -diff
	@go mod verify
	@$(MAKE) verify-generated
	@$(MAKE) lint
	@$(MAKE) shell-lint
	@$(MAKE) workflow-lint
	@$(MAKE) test-unit

audit-secrets: | deps
	@echo ">> scanning reachable Git history for secrets"
	@GOBIN=$(TOOLS_DIR) go install github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION)
	@$(TOOLS_DIR)/gitleaks git . --no-banner --redact
	@echo ">> scanning the current workspace for secrets"
	@$(TOOLS_DIR)/gitleaks dir . --no-banner --redact

audit-licenses:
	@echo ">> auditing dependency licenses"
	@./scripts/audit-licenses.sh

licenses:
	@echo ">> refreshing Go dependency license texts"
	@./scripts/audit-licenses.sh update

audit-node:
	@echo ">> auditing Node.js test dependencies"
	@cd e2e && npm audit --audit-level=high

audit-security: | deps
	@echo ">> running govulncheck"
	@GOBIN=$(TOOLS_DIR) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@GOCACHE=$(GOCACHE) $(TOOLS_DIR)/govulncheck ./...
	@echo ">> running gosec"
	@GOBIN=$(TOOLS_DIR) go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
	@GOCACHE=$(GOCACHE) $(TOOLS_DIR)/gosec -quiet ./...

audit: audit-secrets audit-licenses audit-node audit-security

clean:
	@echo ">> removing build artifacts"
	rm -f $(BIN_DIR)/$(APP) $(INSTALLER_BINARY) $(E2E_BINARY)
	rm -rf "$(CURDIR)/dist"

container-build:
	docker build --pull --tag miniform:local .

container-test: container-build
	@set -eu; \
		container=miniform-test; \
		volume=miniform-test-storage; \
		cleanup() { \
			docker rm --force "$$container" >/dev/null 2>&1 || true; \
			docker volume rm --force "$$volume" >/dev/null 2>&1 || true; \
		}; \
		trap cleanup EXIT INT TERM; \
		cleanup; \
		docker volume create "$$volume" >/dev/null; \
		docker run --rm --user 0:0 --entrypoint sh \
			--volume "$$volume":/app/storage miniform:local \
			-c 'umask 077; : > /app/storage/restored.db'; \
		docker run --detach --name "$$container" \
			--publish 127.0.0.1:18080:8080 \
			--env MINIFORM_ENV=development \
			--volume "$$volume":/app/storage \
			miniform:local >/dev/null; \
		for attempt in $$(seq 1 30); do \
			curl --fail --silent http://127.0.0.1:18080/_health >/dev/null && break; \
			test "$$attempt" -lt 30 || (docker logs "$$container" && exit 1); \
			sleep 1; \
		done; \
		docker exec "$$container" sh -c \
			'test "$$(stat -c %u /app/storage/restored.db)" = 10001'; \
		docker exec "$$container" sh -c \
			'set -- $$(grep "^Uid:" /proc/1/status); test "$$2" = 10001'; \
		echo ">> container health, ownership, and runtime UID checks passed"

apple-container-start:
	@container system status >/dev/null 2>&1 || container system start --enable-kernel-install

apple-container-build: apple-container-start
	container build --platform linux/arm64 --tag $(APPLE_CONTAINER_IMAGE) --file Dockerfile .

apple-container-run: apple-container-build
	@mkdir -p $(APPLE_CONTAINER_STORAGE)
	container run --name $(APPLE_CONTAINER_NAME) --detach --rm \
		--init \
		--publish 127.0.0.1:$(APPLE_CONTAINER_PORT):8080 \
		--env MINIFORM_ENV=development \
		--volume $(APPLE_CONTAINER_STORAGE):/app/storage \
		$(APPLE_CONTAINER_IMAGE)
	@ip=$$(container inspect $(APPLE_CONTAINER_NAME) | jq -r '.[0].status.networks[0].ipv4Address | split("/")[0]'); \
		echo "Miniform: http://$$ip:8080"

apple-container-health:
	@ip=$$(container inspect $(APPLE_CONTAINER_NAME) | jq -r '.[0].status.networks[0].ipv4Address | split("/")[0]'); \
		curl -fsS "http://$$ip:8080/_health"; echo

apple-container-stop:
	@container stop $(APPLE_CONTAINER_NAME) >/dev/null 2>&1 || true

release-check:
	@GOBIN=$(TOOLS_DIR) go install github.com/goreleaser/goreleaser/v2@v2.17.0
	@$(TOOLS_DIR)/goreleaser check

release:
	@if [ -z "$(v)" ]; then \
		echo "Usage: make release v=3.0.5"; \
		exit 1; \
	fi
	@./scripts/validate-release-tag.sh "v$(v)"
	@test -z "$$(git status --porcelain)" || (echo "Error: Uncommitted changes. Commit first." && exit 1)
	@echo "Creating release v$(v)..."
	@$(MAKE) check
	git tag -a "v$(v)" -m "Release v$(v)"
	git push origin "v$(v)"
	@echo ""
	@echo "Release v$(v) triggered!"
	@echo "GoReleaser will build: binaries + multi-arch Docker images + GitHub release"
	@echo "Watch: https://github.com/$$(gh repo view --json nameWithOwner -q .nameWithOwner)/actions"
