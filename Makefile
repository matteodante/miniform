GOCACHE ?= $(CURDIR)/.gocache
BIN_DIR ?= $(CURDIR)/bin
APP      = miniform
TAILWIND = $(BIN_DIR)/tailwindcss
APPLE_CONTAINER_IMAGE ?= miniform:local
APPLE_CONTAINER_NAME ?= miniform
APPLE_CONTAINER_PORT ?= 8080
APPLE_CONTAINER_STORAGE ?= $(CURDIR)/storage

WATCHEXEC ?= $(shell command -v watchexec 2>/dev/null)
GOTESTSUM ?= $(shell command -v gotestsum 2>/dev/null)
TOOLS_DIR ?= $(CURDIR)/.tools
GOLANGCI_LINT_VERSION ?= v2.12.2
GOVULNCHECK_VERSION ?= v1.6.0
GOSEC_VERSION ?= v2.28.0
GITLEAKS_VERSION ?= v8.30.1
ACTIONLINT_VERSION ?= v1.7.12

.PHONY: help bootstrap build run seed dev test test-unit test-e2e test-e2e-setup test-integration tidy fmt fmt-check lint workflow-lint check audit audit-secrets audit-licenses audit-node audit-security verify-generated clean deps release release-check vendor css css-watch container-build container-test apple-container-start apple-container-build apple-container-run apple-container-health apple-container-stop

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
	@echo "  test         - run unit & e2e tests"
	@echo "  test-unit    - run all non-VM Go tests"
	@echo "  test-e2e     - run Playwright end-to-end tests in e2e/"
	@echo "  test-e2e-setup - install Playwright dependencies"
	@echo "  test-integration - run VM-based installer integration tests (requires Multipass)"
	@echo "  check        - run formatting, modules, generated files, lint, and Go tests"
	@echo "  workflow-lint - validate GitHub Actions workflows"
	@echo "  audit        - scan secrets, licenses, Go/Node vulnerabilities, and Go security"
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

test: test-unit test-e2e

test-unit: deps
ifeq ($(strip $(GOTESTSUM)),)
	@echo ">> go test ./..."
	MINIFORM_ENV=test GOCACHE=$(GOCACHE) go test ./...
else
	@echo ">> gotestsum ./..."
	MINIFORM_ENV=test GOCACHE=$(GOCACHE) $(GOTESTSUM) --format testname -- -count=1 ./...
endif

test-e2e-setup:
	@echo ">> installing Playwright dependencies"
	cd e2e && npm ci && npx playwright install chromium

test-e2e: deps
	@echo ">> running Playwright E2E tests"
	cd e2e && npm test

test-integration: build
	@echo ">> running VM-based installer integration tests"
	@echo "   (requires Multipass: brew install multipass)"
	MINIFORM_ENV=test MINIFORM_RUN_INSTALLATION_TEST=1 BINARY_PATH=$(BIN_DIR)/$(APP) go test -v -timeout 15m ./tests/integration/...

tidy: deps
	GOCACHE=$(GOCACHE) go mod tidy

fmt:
	@echo ">> formatting Go files"
	go fmt ./...

fmt-check:
	@echo ">> checking Go formatting"
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.gocache/*' -not -path './.tools/*'))" || \
		(echo "Go files need formatting; run 'make fmt'"; gofmt -l $$(find . -name '*.go' -not -path './.gocache/*' -not -path './.tools/*'); exit 1)

$(TOOLS_DIR)/golangci-lint: | deps
	GOBIN=$(TOOLS_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: $(TOOLS_DIR)/golangci-lint
	@echo ">> running golangci-lint"
	GOCACHE=$(GOCACHE) $(TOOLS_DIR)/golangci-lint run

workflow-lint: | deps
	@echo ">> validating GitHub Actions workflows"
	@GOBIN=$(TOOLS_DIR) go install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)
	@$(TOOLS_DIR)/actionlint -shellcheck= .github/workflows/*.yml

verify-generated: css
	@echo ">> checking generated and vendored assets"
	@git diff --exit-code -- web/static/app.built.css web/static/vendor

check: fmt-check
	@echo ">> verifying Go modules"
	@go mod tidy
	@git diff --exit-code -- go.mod go.sum
	@go mod verify
	@$(MAKE) verify-generated
	@$(MAKE) lint
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
	rm -f $(BIN_DIR)/$(APP)

container-build:
	docker build --pull --tag miniform:local .

container-test: container-build
	@docker rm --force miniform-test >/dev/null 2>&1 || true
	docker run --detach --name miniform-test \
		--publish 127.0.0.1:18080:8080 \
		--env MINIFORM_ENV=development \
		--tmpfs /app/storage:uid=10001,gid=10001,mode=700 \
		miniform:local
	@for attempt in $$(seq 1 30); do \
		curl --fail --silent http://127.0.0.1:18080/_health >/dev/null && break; \
		test "$$attempt" -lt 30 || (docker logs miniform-test && exit 1); \
		sleep 1; \
	done
	@docker rm --force miniform-test >/dev/null
	@echo ">> container health check passed"

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
	@echo "Creating release v$(v)..."
	@git diff --quiet && git diff --cached --quiet || (echo "Error: Uncommitted changes. Commit first." && exit 1)
	@$(MAKE) check
	git tag -a "v$(v)" -m "Release v$(v)"
	git push origin "v$(v)"
	@echo ""
	@echo "Release v$(v) triggered!"
	@echo "GoReleaser will build: binaries + multi-arch Docker images + GitHub release"
	@echo "Watch: https://github.com/$$(gh repo view --json nameWithOwner -q .nameWithOwner)/actions"
