# The compose stack. The project name and every published port derive from the checkout's path, so worktrees
# running their gates side by side never share a database or collide on a port.

STACK_HASH := $(shell printf '%s' '$(ROOT)' | cksum | cut -d' ' -f1)
STACK_PROJECT := exciter-$(STACK_HASH)
STACK_NET := $(STACK_PROJECT)_default

# Twenty ports per checkout from 10000 to 19999, clear of Stator's range and the kernel's ephemeral ports;
# scripts/stack-ports.sh holds the rule and tests/test-ports.sh checks it.
STACK_PORT_BASE ?= $(shell $(ROOT)/scripts/stack-ports.sh '$(ROOT)')
stack_port = $(shell echo $$(($(STACK_PORT_BASE) + $(1))))

WEB_PORT              := $(call stack_port,0)
API_PORT              := $(call stack_port,1)
KEYCLOAK_PORT         := $(call stack_port,2)
MAILPIT_PORT          := $(call stack_port,3)
POSTGRES_PORT         := $(call stack_port,4)
POSTGRES_REPLICA_PORT := $(call stack_port,5)
VALKEY_PORT           := $(call stack_port,6)
S3_PORT               := $(call stack_port,7)
ARMATURE_STUB_PORT    := $(call stack_port,8)
STATOR_STUB_PORT      := $(call stack_port,9)

# Credentials for the throwaway development stack; override them in the environment for anything else.
POSTGRES_PASSWORD  ?= exciter
APP_DB_PASSWORD    ?= exciter_app
ADMIN_DB_PASSWORD  ?= exciter_admin
S3_ACCESS_KEY      ?= exciter
S3_SECRET_KEY      ?= exciter-dev-secret
VALKEY_PASSWORD    ?= exciter_valkey
# Seals stored secrets, as the compose file's default does. The integration suite seals the Armature tokens it stores
# with it too, so the stack's worker can open them.
EXCITER_SECRET_KEY ?= ZXhjaXRlci1kZXZlbG9wbWVudC1vbmx5LXNlY3JldC0=

KEYCLOAK_REALM := suite-dev

# The suite writes to a bucket of its own, so what it leaves behind never shows up among the running app's files.
STACK_TEST_BUCKET := exciter-test

export COMPOSE_FILE := $(ROOT)/deploy/docker-compose.yml
export COMPOSE_PROJECT_NAME := $(STACK_PROJECT)
export WEB_PORT API_PORT KEYCLOAK_PORT MAILPIT_PORT POSTGRES_PORT POSTGRES_REPLICA_PORT VALKEY_PORT S3_PORT ARMATURE_STUB_PORT STATOR_STUB_PORT
export POSTGRES_PASSWORD APP_DB_PASSWORD ADMIN_DB_PASSWORD S3_ACCESS_KEY S3_SECRET_KEY VALKEY_PASSWORD
export EXCITER_SECRET_KEY

STACK_PG_URL = postgres://$(1):$(2)@$(3):5432/exciter?sslmode=disable

# The Go toolchain container on the stack's network, with everything the integration suite reads. Reads go to the
# replica as they do in the running api, so the suite proves read-your-writes rather than stepping around it.
DOCKER_GO_STACK = $(call go_run,--network $(STACK_NET) \
	-e EXCITER_DB_PRIMARY_URL='$(call STACK_PG_URL,exciter_app,$(APP_DB_PASSWORD),postgres-primary)' \
	-e EXCITER_DB_REPLICA_URLS='$(call STACK_PG_URL,exciter_app,$(APP_DB_PASSWORD),postgres-replica)' \
	-e EXCITER_DB_ADMIN_URL='$(call STACK_PG_URL,exciter_admin,$(ADMIN_DB_PASSWORD),postgres-primary)' \
	-e EXCITER_VALKEY_URL='redis://:$(VALKEY_PASSWORD)@valkey:6379/0' \
	-e EXCITER_TEST_SUPERUSER_URL='$(call STACK_PG_URL,exciter,$(POSTGRES_PASSWORD),postgres-primary)' \
	-e EXCITER_TEST_REPLICA_SUPERUSER_URL='$(call STACK_PG_URL,exciter,$(POSTGRES_PASSWORD),postgres-replica)' \
	-e EXCITER_TEST_KEYCLOAK_URL=http://keycloak:8080 \
	-e EXCITER_S3_ENDPOINT=seaweedfs:8333 \
	-e EXCITER_S3_BUCKET=$(STACK_TEST_BUCKET) \
	-e EXCITER_S3_ACCESS_KEY='$(S3_ACCESS_KEY)' \
	-e EXCITER_S3_SECRET_KEY='$(S3_SECRET_KEY)' \
	-e EXCITER_TEST_ARMATURE_URL=http://localhost:$(ARMATURE_STUB_PORT) \
	-e EXCITER_TEST_ARMATURE_STUB_URL=http://armature-stub:8080 \
	-e EXCITER_TEST_STATOR_URL=http://localhost:$(STATOR_STUB_PORT) \
	-e EXCITER_TEST_STATOR_STUB_URL=http://stator-stub:8080 \
	-e EXCITER_TEST_SECRET_KEY='$(EXCITER_SECRET_KEY)' \
	-e EXCITER_TEST_APP_URL=http://localhost:$(WEB_PORT) \
	-e EXCITER_TEST_MAILPIT_URL=http://mailpit:8025 \
	-e EXCITER_TEST_WEB_STACK_URL=http://web \
	-e EXCITER_TEST_BUILD_VERSION='$(EXCITER_BUILD_VERSION)' \
	-e EXCITER_TEST_BUILD_COMMIT='$(EXCITER_BUILD_COMMIT)' \
	$(1))

# What the stack publishes, for the test suites and for people: a shell can source it, and so can a Playwright config.
STACK_ENV_FILE := $(ROOT)/.cache/stack.env

.PHONY: stack-env
stack-env:
	@mkdir -p $(dir $(STACK_ENV_FILE))
	@printf '%s\n' \
		'COMPOSE_PROJECT_NAME=$(STACK_PROJECT)' \
		'STACK_NETWORK=$(STACK_NET)' \
		'WEB_PORT=$(WEB_PORT)' \
		'API_PORT=$(API_PORT)' \
		'KEYCLOAK_PORT=$(KEYCLOAK_PORT)' \
		'MAILPIT_PORT=$(MAILPIT_PORT)' \
		'POSTGRES_PORT=$(POSTGRES_PORT)' \
		'POSTGRES_REPLICA_PORT=$(POSTGRES_REPLICA_PORT)' \
		'VALKEY_PORT=$(VALKEY_PORT)' \
		'S3_PORT=$(S3_PORT)' \
		'ARMATURE_STUB_PORT=$(ARMATURE_STUB_PORT)' \
		'STATOR_STUB_PORT=$(STATOR_STUB_PORT)' \
		'EXCITER_WEB_URL=http://localhost:$(WEB_PORT)' \
		'EXCITER_KEYCLOAK_URL=http://localhost:$(KEYCLOAK_PORT)' \
		'EXCITER_OIDC_ISSUER=http://localhost:$(KEYCLOAK_PORT)/realms/$(KEYCLOAK_REALM)' \
		'EXCITER_MAILPIT_URL=http://localhost:$(MAILPIT_PORT)' \
		'EXCITER_ARMATURE_URL=http://localhost:$(ARMATURE_STUB_PORT)' \
		'EXCITER_STATOR_URL=http://localhost:$(STATOR_STUB_PORT)' \
		'EXCITER_BUILD_VERSION=$(EXCITER_BUILD_VERSION)' \
		'EXCITER_BUILD_COMMIT=$(EXCITER_BUILD_COMMIT)' \
		'EXCITER_TEST_SUPERUSER_URL=postgres://exciter:$(POSTGRES_PASSWORD)@127.0.0.1:$(POSTGRES_PORT)/exciter?sslmode=disable' \
		> $(STACK_ENV_FILE)

.PHONY: up
up: stack-env ## Build and start the whole stack in the background, and say where it is
	docker compose up -d --build --wait
	@echo "Web        http://localhost:$(WEB_PORT)"
	@echo "API        http://localhost:$(API_PORT)/api/v1/meta"
	@echo "Keycloak   http://localhost:$(KEYCLOAK_PORT) (admin / admin; realm $(KEYCLOAK_REALM))"
	@echo "Mailpit    http://localhost:$(MAILPIT_PORT)"
	@echo "Postgres   127.0.0.1:$(POSTGRES_PORT) (replica $(POSTGRES_REPLICA_PORT))"
	@echo "Valkey     127.0.0.1:$(VALKEY_PORT)"
	@echo "S3         http://localhost:$(S3_PORT)"
	@echo "Armature   http://localhost:$(ARMATURE_STUB_PORT) (the stub; tokens are armature_pat_<org slug>_<person>)"
	@echo "Stator     http://localhost:$(STATOR_STUB_PORT) (the stub; tokens are stator_pat_<tenant>_<person>)"
	@echo "Ports and project name are in $(STACK_ENV_FILE)."

.PHONY: down
down: ## Stop the stack, keeping its data
	docker compose down

.PHONY: clean
clean: ## Stop the stack and delete its volumes
	docker compose down -v --remove-orphans
	@rm -f $(STACK_ENV_FILE)

.PHONY: logs
logs: ## Follow the stack's logs: make logs S=keycloak for one service
	docker compose logs -f $(S)

.PHONY: ports
ports: ## Print this checkout's compose project and ports
	@echo "project $(STACK_PROJECT), ports $(STACK_PORT_BASE) to $(shell echo $$(($(STACK_PORT_BASE) + 19)))"

.PHONY: shell
shell: | $(GO_CACHE) ## Interactive shell in the Go toolchain container, on the stack's network
	$(call DOCKER_GO_STACK,-it) sh

.PHONY: seed
seed: ## Run the seed again against the running stack
	docker compose run --rm seed

.PHONY: stack-up
stack-up: stack-env ## The gate's stack: build and start it, and wait until every service is healthy
	docker compose up -d --build --wait --quiet-pull

.PHONY: stack-down
stack-down: ## Remove the gate's stack and its volumes
	docker compose down -v --remove-orphans
	@rm -f $(STACK_ENV_FILE)
