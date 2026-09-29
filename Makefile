ENV_FILE ?= infra/.env
CORP_CA  := $(if $(shell grep -sE '^RAG_CA_BUNDLE=.' "$(ENV_FILE)"),-f infra/docker-compose.corp-ca.yml)
LOCAL_CO := $(if $(wildcard infra/docker-compose.local.yml),-f infra/docker-compose.local.yml)
TLS_CO   := $(if $(shell grep -sE '^FRONTEND_TLS_CERT=.' "$(ENV_FILE)"),-f infra/docker-compose.https.yml)
COMPOSE  := docker compose --env-file "$(ENV_FILE)" -f infra/docker-compose.yml $(CORP_CA) $(LOCAL_CO) $(TLS_CO)
SERVICES := backend/archive-worker backend/auth backend/chunk-splitter backend/db-parser \
            backend/db-service backend/email-parser backend/llm-service backend/main-service \
            backend/ocr-service backend/office-parser backend/pdf-parser backend/vlm-service backend/source-harvester \
            backend/trend-sources
PLATFORM := backend/platform
GOBIN    ?= $(shell if command -v go >/dev/null 2>&1; then go env GOPATH; else printf '%s/go' "$$HOME"; fi)/bin
COVER_MIN ?= 80
PYTHON   ?= python3
ENV_EXAMPLE := infra/.env.example

FRONTEND       := frontend
BUN            := bun
# Шлюз, куда dev-сервер SPA проксирует /api и /ws; по умолчанию — шлюз `make up`.
# Переопределение: make frontend-dev HTTP_PORT=9000 или RAG_API=http://host:port.
HTTP_PORT      ?= 8080
RAG_API        ?= http://localhost:$(HTTP_PORT)
FRONTEND_PORT  ?= 5173
FRONTEND_STAMP := $(FRONTEND)/node_modules/.install.stamp

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## go mod tidy in the platform and every service module
	cd $(PLATFORM) && go mod tidy
	@for s in $(SERVICES); do echo "tidy $$s"; (cd $$s && go mod tidy) || exit 1; done

.PHONY: build
build: ## go build ./... for the platform and every service
	cd $(PLATFORM) && go build ./...
	@for s in $(SERVICES); do echo "build $$s"; (cd $$s && go build ./...) || exit 1; done

.PHONY: vet
vet: ## go vet every module
	cd $(PLATFORM) && go vet ./...
	@for s in $(SERVICES); do echo "vet $$s"; (cd $$s && go vet ./...) || exit 1; done

# internal/platform сервисов — копии platform/pkg (make vendor): их тесты уже прошли в платформе,
# а повтор в каждом сервисе занимал большую часть времени. Сборку, vet, линт и покрытие копии проходят как прежде.
SERVICE_PKGS = go list ./... | grep -vE '/internal/platform(/|$$)'

.PHONY: test
test: ## go test ./... for the platform and every service; vendored platform copies are tested once, in the platform
	cd $(PLATFORM) && go test ./...
	@for s in $(SERVICES); do echo "test $$s"; (cd $$s && go test $$($(SERVICE_PKGS))) || exit 1; done

.PHONY: test-race
test-race: ## go test -race ./... for the platform and every service
	cd $(PLATFORM) && go test -race ./...
	@for s in $(SERVICES); do echo "test-race $$s"; (cd $$s && go test -race ./...) || exit 1; done

.PHONY: lint
lint: ## golangci-lint every module (sequential: the linter holds a global lock)
	cd $(PLATFORM) && $(GOBIN)/golangci-lint run ./...
	@for s in $(SERVICES); do echo "lint $$s"; (cd $$s && $(GOBIN)/golangci-lint run ./...) || exit 1; done

.PHONY: proto
proto: ## Regenerate Go and Python stubs from backend/proto (needs protoc + grpcio-tools)
	PYTHON=$(PYTHON) bash scripts/gen-proto.sh

.PHONY: vendor
vendor: ## Copy platform/pkg (and generated stubs) into every service's internal/platform
	bash scripts/vendor-platform.sh

.PHONY: fmt
fmt: ## gofmt the whole tree
	gofmt -w $(SERVICES) $(PLATFORM)/pkg

.PHONY: prereq
prereq: ## Check host prerequisites for the Docker stack
	@command -v docker >/dev/null 2>&1 || { echo "error: docker is not installed"; exit 1; }
	@docker compose version >/dev/null 2>&1 || { echo "error: Docker Compose v2 is required"; exit 1; }
	@docker info >/dev/null 2>&1 || { echo "error: Docker daemon is not reachable"; exit 1; }
	@docker info --format '{{json .Runtimes}}' 2>/dev/null | grep -q '"nvidia"' || { \
		echo "error: Docker runtime 'nvidia' is required for the GPU services."; \
		echo "       Install NVIDIA Container Toolkit, then retry 'make up'."; \
		exit 1; \
	}

.PHONY: env
env: env-bootstrap ## Create infra/.env and fill missing local secrets

.PHONY: public-source
public-source: ## Export current source and reviewed presentations without history or private runtime files
	$(PYTHON) scripts/export-public-source.py

.PHONY: env-bootstrap
env-bootstrap:
	@set -eu; \
	if [ ! -f "$(ENV_EXAMPLE)" ]; then \
		echo "error: $(ENV_EXAMPLE) not found"; exit 1; \
	fi; \
	created=0; changed=0; \
	if [ ! -f "$(ENV_FILE)" ]; then \
		cp "$(ENV_EXAMPLE)" "$(ENV_FILE)"; \
		created=1; changed=1; \
	fi; \
	chmod 600 "$(ENV_FILE)"; \
	rand_hex() { od -An -N"$$1" -tx1 /dev/urandom | tr -d ' \n'; }; \
	set_if_blank() { \
		key="$$1"; value="$$2"; \
		if grep -q "^$${key}=" "$(ENV_FILE)"; then \
			if grep -q "^$${key}=$$" "$(ENV_FILE)"; then \
				tmp=$$(mktemp); \
				awk -v k="$$key" -v v="$$value" 'BEGIN{done=0} $$0 ~ "^" k "=" && done == 0 { print k "=" v; done=1; next } { print }' "$(ENV_FILE)" > "$$tmp"; \
				mv "$$tmp" "$(ENV_FILE)"; \
				changed=1; \
			fi; \
		else \
			printf '\n%s=%s\n' "$$key" "$$value" >> "$(ENV_FILE)"; \
			changed=1; \
		fi; \
	}; \
	set_if_blank JWT_SECRET "$$(rand_hex 48)"; \
	set_if_blank ADMIN_PASSWORD "$$(rand_hex 18)"; \
	set_if_blank GRAFANA_ADMIN_PASSWORD "$$(rand_hex 18)"; \
	set_if_blank RABBITMQ_PASSWORD "$$(rand_hex 18)"; \
	set_if_blank POSTGRES_PASSWORD "$$(rand_hex 18)"; \
	set_if_blank S3_ACCESS_KEY "rag$$(rand_hex 8)"; \
	set_if_blank S3_SECRET_KEY "$$(rand_hex 32)"; \
	set_if_blank RENDER_TOKEN "$$(rand_hex 32)"; \
	if [ "$$created" = 1 ]; then \
		echo "created $(ENV_FILE) with generated local secrets"; \
	elif [ "$$changed" = 1 ]; then \
		echo "filled missing secrets in $(ENV_FILE)"; \
	fi

.PHONY: up
up: prereq env-bootstrap ## Build images and start the full stack (including frontend on FRONTEND_PORT)
	$(COMPOSE) up -d --build
	@front_port=$$(awk -F= '/^FRONTEND_PORT=/{print $$2; exit}' "$(ENV_FILE)"); \
	api_port=$$(awk -F= '/^HTTP_PORT=/{print $$2; exit}' "$(ENV_FILE)"); \
	echo "frontend: http://localhost:$${front_port:-5173}"; \
	echo "api gateway: http://localhost:$${api_port:-8080}"; \
	echo "admin login: admin / see ADMIN_PASSWORD in $(ENV_FILE)"

.PHONY: up-logging
up-logging: prereq env-bootstrap ## Start the stack including the Vector log collector
	$(COMPOSE) --profile logging up -d --build
	@echo "admin login: admin / see ADMIN_PASSWORD in $(ENV_FILE)"

# BUILDX_BUILDER=default у deploy: на стенде настроен builder multiarch, который
# не поднимается (runc не находит библиотеку NVIDIA), и сборка падает ещё до
# первого слоя. Многоархитектурных образов проект не собирает.
.PHONY: deploy
deploy: ## Точечно пересобрать и поднять сервисы на стенде: make deploy SERVICES="frontend main-service"
	@test "$(origin SERVICES)" = "command line" || { echo 'укажите сервисы: make deploy SERVICES="frontend main-service"'; exit 1; }
	@test -n "$(strip $(SERVICES))" || { echo 'SERVICES пуст — иначе поднимется весь стек'; exit 1; }
	BUILDX_BUILDER=default $(COMPOSE) up -d --build --no-deps $(SERVICES)

.PHONY: prereq-soft
prereq-soft: ## Проверить хост под запуск без требования GPU
	@command -v docker >/dev/null 2>&1 || { echo "error: docker is not installed"; exit 1; }
	@docker compose version >/dev/null 2>&1 || { echo "error: Docker Compose v2 is required"; exit 1; }
	@docker info >/dev/null 2>&1 || { echo "error: Docker daemon is not reachable"; exit 1; }

.PHONY: bootstrap
bootstrap: prereq-soft env-bootstrap ## Развернуть контур с нуля: окружение, данные из BUNDLE=..., сборка, старт, ожидание здоровья
	@set -eu; \
	if [ -n "$(BUNDLE)" ]; then infra/scripts/bundle.sh import "$(BUNDLE)" $(FLAGS); fi; \
	gpu=""; \
	if ! docker info --format '{{json .Runtimes}}' 2>/dev/null | grep -q '"nvidia"'; then \
		gpu="-f infra/docker-compose.nogpu.yml"; \
		echo "nvidia runtime не найден: нейросервисы поднимаются на CPU (infra/docker-compose.nogpu.yml)"; \
	fi; \
	$(COMPOSE) $$gpu up -d --build
	@$(MAKE) wait-healthy
	@front_port=$$(awk -F= '/^FRONTEND_PORT=/{print $$2; exit}' "$(ENV_FILE)"); \
	echo "frontend: http://localhost:$${front_port:-5173}"; \
	echo "admin login: admin / see ADMIN_PASSWORD in $(ENV_FILE)"

# Радар без конвейера документов, фоновых воркеров, распознавания, реранкера и мониторинга:
# остальные зависимости (хранилища, auth, main-service, сборщики) compose поднимает сам.
LAPTOP_SERVICES := frontend radar-collector cn-patent-collector page-renderer
LAPTOP_CO := -f infra/docker-compose.nogpu.yml -f infra/docker-compose.laptop.yml

.PHONY: laptop
laptop: prereq-soft env-bootstrap ## Радар на ноутбуке без GPU; генерацию задают VLLM_URL/VLLM_MODEL/VLLM_API_KEY в infra/.env
	@grep -qE '^VLLM_URL=.' "$(ENV_FILE)" || echo "внимание: VLLM_URL пуст — без модели генерации радар не напишет ни одной карточки"
	$(COMPOSE) $(LAPTOP_CO) up -d --build $(LAPTOP_SERVICES)
	@front_port=$$(awk -F= '/^FRONTEND_PORT=/{print $$2; exit}' "$(ENV_FILE)"); \
	echo "frontend: http://localhost:$${front_port:-5173}"; \
	echo "admin login: admin / see ADMIN_PASSWORD in $(ENV_FILE)"; \
	echo "сборщики и окно arXiv собирают первый снимок в фоне: до его готовности поиск отвечает 503"

.PHONY: wait-healthy
# Сборщики источников здоровы только с собранным снимком: первый сбор на пустом томе дольше срока
# ожидания, а радар их отсутствие переживает оговоркой, поэтому готовность контура их не ждёт.
wait-healthy: ## Дождаться, пока контейнеры с проверкой здоровья станут healthy (WAIT_MINUTES=20)
	@set -eu; \
	deadline=$$(( $$(date +%s) + 60 * $${WAIT_MINUTES:-20} )); \
	while :; do \
		bad=$$($(COMPOSE) ps --format '{{.Name}}\t{{.Status}}' | grep -E 'health: starting|unhealthy' | grep -v -- '-collector-' || true); \
		[ -z "$$bad" ] && break; \
		if [ $$(date +%s) -ge $$deadline ]; then \
			echo "не дождались готовности:"; echo "$$bad"; exit 1; \
		fi; \
		sleep 10; \
	done; \
	echo "проверки здоровья пройдены"

.PHONY: bundle-export
bundle-export: ## Выгрузить данные для переноса: make bundle-export OUT=/path [FLAGS="--with-cache --with-models"]
	@test -n "$(OUT)" || { echo 'укажите каталог: make bundle-export OUT=/path/to/bundle'; exit 1; }
	infra/scripts/bundle.sh export "$(OUT)" $(FLAGS)

.PHONY: bundle-import
bundle-import: env-bootstrap ## Восстановить данные из бандла: make bundle-import BUNDLE=/path [FLAGS=--force]
	@test -n "$(BUNDLE)" || { echo 'укажите каталог: make bundle-import BUNDLE=/path/to/bundle'; exit 1; }
	infra/scripts/bundle.sh import "$(BUNDLE)" $(FLAGS)

.PHONY: bundle-verify
bundle-verify: ## Сверить контрольные суммы бандла: make bundle-verify BUNDLE=/path
	@test -n "$(BUNDLE)" || { echo 'укажите каталог: make bundle-verify BUNDLE=/path/to/bundle'; exit 1; }
	infra/scripts/bundle.sh verify "$(BUNDLE)"

.PHONY: down
down: ## Stop the stack
	$(COMPOSE) down

.PHONY: clean
clean: ## Stop the stack and delete all volumes (DATA LOSS)
	$(COMPOSE) down -v

.PHONY: logs
logs: ## Tail logs from all containers
	$(COMPOSE) logs -f

.PHONY: ps
ps: ## Show container status
	$(COMPOSE) ps

.PHONY: hooks
hooks: frontend-install ## Install git hooks (frontend format/lint/types/knip)
	cd $(FRONTEND) && bun run hooks:install

.PHONY: s3-drift-check
s3-drift-check: ## Check that every Postgres document row has a matching S3 object
	python scripts/tools/data/check_s3_db_drift.py

.PHONY: demo-smoke
demo-smoke: ## Check demo readiness without spending generation LLM calls
	python scripts/tools/demo/demo_smoke.py

.PHONY: demo-smoke-full
demo-smoke-full: ## Check demo readiness including one full RAG answer
	python scripts/tools/demo/demo_smoke.py --full-rag

.PHONY: demo-ui-smoke
demo-ui-smoke: ## Check the served frontend bundle and visible hypotheses board
	DEMO_FRONTEND_URL=http://localhost:$(FRONTEND_PORT) python scripts/tools/demo/demo_smoke.py \
		--check-frontend \
		--min-visible-hypotheses 20 \
		--check-visible-hypothesis-quality

.PHONY: demo-env-check
demo-env-check: ## Check demo env without printing secret values
	python scripts/tools/demo/demo_smoke.py \
		--check-env \
		--require-generation-env \
		--env-only

.PHONY: demo-security-check
demo-security-check: ## Strict env/secret check for non-local exposure
	python scripts/tools/demo/demo_smoke.py \
		--check-env \
		--require-generation-env \
		--strict-secrets \
		--env-only

.PHONY: demo-rotate-secrets
demo-rotate-secrets: ## Rotate local demo JWT/admin secrets and update the Postgres admin hash
	python scripts/tools/demo/rotate_demo_secrets.py

.PHONY: demo-resilience
demo-resilience: ## Check critical RAG graceful-degradation unit tests
	cd backend/llm-service && go test ./internal/application \
		-run 'TestGenerationFailureReturnsExtractiveFallbackForChat|TestGenerationFailurePropagatesForStructuredPrompt|TestP0_2_DegradedAbstainNotCached'

.PHONY: config
config: env-bootstrap ## Validate the compose file
	$(COMPOSE) config -q && echo "compose OK"

.PHONY: frontend-check-bun
frontend-check-bun:
	@command -v $(BUN) >/dev/null 2>&1 || { \
		echo "error: '$(BUN)' not found in PATH. Install bun: https://bun.sh"; exit 1; }

# node_modules вне git, поэтому целью служит штамп: установка — только при смене манифеста или lock-файла.
$(FRONTEND_STAMP): $(FRONTEND)/package.json $(FRONTEND)/bun.lock | frontend-check-bun
	cd $(FRONTEND) && $(BUN) install --frozen-lockfile --registry https://registry.npmjs.org --network-concurrency 4
	@touch $@

.PHONY: frontend-install
frontend-install: $(FRONTEND_STAMP) ## Install frontend deps (bun install, only when lockfile changes)

.PHONY: frontend-dev
frontend-dev: frontend-install ## Run local Vite dev server on :5173 (proxies /api to RAG_API)
	cd $(FRONTEND) && RAG_API=$(RAG_API) $(BUN) run dev --host

.PHONY: frontend-build
frontend-build: frontend-install ## Type-check + production build of the SPA into frontend/dist
	cd $(FRONTEND) && RAG_API=$(RAG_API) $(BUN) run build

.PHONY: frontend-preview
frontend-preview: frontend-build ## Preview the production SPA build locally
	cd $(FRONTEND) && RAG_API=$(RAG_API) $(BUN) run preview --host

.PHONY: frontend-typecheck
frontend-typecheck: frontend-install ## Type-check the SPA (tsc)
	cd $(FRONTEND) && $(BUN) run typecheck

.PHONY: frontend-lint
frontend-lint: frontend-install ## Lint the SPA (oxlint + project conventions)
	cd $(FRONTEND) && $(BUN) run lint

.PHONY: frontend-fmt
frontend-fmt: frontend-install ## Format the SPA (oxfmt)
	cd $(FRONTEND) && $(BUN) run fmt

.PHONY: frontend-fmt-check
frontend-fmt-check: frontend-install ## Check SPA formatting without writing (oxfmt --check)
	cd $(FRONTEND) && $(BUN) run fmt:check

.PHONY: frontend-check
frontend-check: frontend-install ## Run the complete frontend verification gate
	cd $(FRONTEND) && $(BUN) run verify

.PHONY: frontend-ui-check
frontend-ui-check: frontend-install ## Verify frontend and run the independent UI kit browser suite
	cd $(FRONTEND) && $(BUN) run verify:ui

.PHONY: dev
dev: ## Start backend containers, then run local Vite frontend dev server
	$(COMPOSE) up -d --build --scale frontend=0
	$(MAKE) frontend-dev

.PHONY: quality quality-unit quality-integration
quality: ## Local release gate, including mandatory isolated integrations
	bash scripts/quality/gate.sh all
quality-unit: ## Local code/runtime checks; explicitly excludes live integrations
	bash scripts/quality/gate.sh unit
quality-integration: ## Mandatory PostgreSQL/RabbitMQ/S3/Qdrant/OpenSearch/Valkey checks
	bash scripts/quality/gate.sh integration
