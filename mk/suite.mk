# Exciter, Armature and Stator together on one Keycloak, for trying the integration end to end (#31). Each product
# runs its own stack from its own checkout, with an override from deploy/suite/; docs/suite.md says how they meet.

SUITE_NET := exciter-suite
# Fixed ports from 30000, clear of Exciter's 10000 to 19999, Stator's 20000 to 29999 and the kernel's ephemeral
# ports from 32768, so docs/suite.md can name them; tests/test-suite.sh checks they stay apart.
SUITE_KEYCLOAK_PORT := 30000
SUITE_EXCITER_WEB_PORT := 30010
SUITE_ARMATURE_WEB_PORT := 30020
SUITE_STATOR_WEB_PORT := 30040
SUITE_ENV_FILE := $(ROOT)/.cache/suite.env
SUITE_SMOKE_CONFIG := playwright.suite.config.ts
# DEMO=1 adds the suite demo to all three as well (#270): Armature's issues, Stator's pages, Exciter's requirements.
DEMO ?=

# Every product's compose with its suite override, project name and ports; the rest of each port block follows the
# product's own order, so a database is still where its owner's docs say relative to the web port.
suite_compose = COMPOSE_PROJECT_NAME=suite-$(1) COMPOSE_FILE=$(2)/deploy/docker-compose.yml:$(ROOT)/deploy/suite/$(1).yml \
	SUITE_KEYCLOAK_PORT=$(SUITE_KEYCLOAK_PORT) SUITE_ARMATURE_WEB_PORT=$(SUITE_ARMATURE_WEB_PORT) SUITE_STATOR_WEB_PORT=$(SUITE_STATOR_WEB_PORT) $(3) docker compose

SUITE_EXCITER = $(call suite_compose,exciter,$(ROOT),\
	WEB_PORT=30010 API_PORT=30011 KEYCLOAK_PORT=$(SUITE_KEYCLOAK_PORT) MAILPIT_PORT=30013 POSTGRES_PORT=30014 \
	POSTGRES_REPLICA_PORT=30015 VALKEY_PORT=30016 S3_PORT=30017 ARMATURE_STUB_PORT=30018 STATOR_STUB_PORT=30019)
SUITE_ARMATURE = $(call suite_compose,armature,$(ARMATURE_DIR),\
	WEB_PORT=30020 API_PORT=30021 KEYCLOAK_PORT=30022 MAILPIT_UI_PORT=30023 POSTGRES_PORT=30024 \
	POSTGRES_REPLICA_PORT=30025 REDIS_PORT=30026 S3_PORT=30027 S3_FILER_PORT=30028 GITEA_PORT=30029 \
	WEB_E2E_PORT=30030 JAEGER_PORT=30031 PROMETHEUS_PORT=30032)
SUITE_STATOR = $(call suite_compose,stator,$(STATOR_DIR),\
	WEB_PORT=30040 API_PORT=30041 KEYCLOAK_PORT=30042 MAILPIT_PORT=30043 POSTGRES_PORT=30044 \
	POSTGRES_REPLICA_PORT=30045 VALKEY_PORT=30046 S3_PORT=30047 ARMATURE_STUB_PORT=30048)

define suite_dirs
	@[ -f "$(ARMATURE_DIR)/deploy/docker-compose.yml" ] || { echo "Name an Armature checkout: make $@ ARMATURE_DIR=~/repos/Armature STATOR_DIR=..."; exit 1; }
	@[ -f "$(STATOR_DIR)/deploy/docker-compose.yml" ] || { echo "Name a Stator checkout: make $@ ARMATURE_DIR=... STATOR_DIR=~/repos/Stator"; exit 1; }
endef

.PHONY: suite-up
suite-up: ## Start Exciter, Armature and Stator on one Keycloak and connect them: ARMATURE_DIR=<checkout> STATOR_DIR=<checkout> [DEMO=1]
	$(suite_dirs)
	@docker network inspect $(SUITE_NET) >/dev/null 2>&1 || docker network create $(SUITE_NET) >/dev/null
	$(SUITE_EXCITER) up -d --build --wait --quiet-pull
	$(SUITE_ARMATURE) up -d --build --wait --quiet-pull
	@# Armature's seed sets an installation up once and refuses one somebody set up, its own run included.
	@if docker run --rm --network host $(CURL_IMAGE) -fsS http://localhost:30021/api/v1/auth/signup | grep -q '"first":true'; then \
		$(SUITE_ARMATURE) run --rm --build seed; \
	else echo "Armature is set up already; its seed is left out."; fi
	$(SUITE_STATOR) up -d --build --wait --quiet-pull
	@$(MAKE) --no-print-directory suite-seed
	@echo "Keycloak   http://localhost:$(SUITE_KEYCLOAK_PORT) (admin / admin; realm dynamo; alice / alice password)"
	@echo "Exciter    http://localhost:$(SUITE_EXCITER_WEB_PORT)"
	@echo "Armature   http://localhost:$(SUITE_ARMATURE_WEB_PORT)"
	@echo "Stator     http://localhost:$(SUITE_STATOR_WEB_PORT)"
	@echo "Tokens, secrets and addresses are in $(SUITE_ENV_FILE)."

# The connections, members, tokens and webhooks, made through each product's API; it runs again harmlessly.
.PHONY: suite-seed
suite-seed: | $(GO_CACHE) ## Connect the running suite's products to each other again
	@mkdir -p $(dir $(SUITE_ENV_FILE))
	$(call go_run,--network $(SUITE_NET) \
		-e SUITE_KEYCLOAK_PORT=$(SUITE_KEYCLOAK_PORT) -e SUITE_EXCITER_WEB_PORT=$(SUITE_EXCITER_WEB_PORT) \
		-e SUITE_ARMATURE_WEB_PORT=$(SUITE_ARMATURE_WEB_PORT) -e SUITE_STATOR_WEB_PORT=$(SUITE_STATOR_WEB_PORT) \
		-e SUITE_ENV_FILE=/work/.cache/suite.env -e SUITE_DEMO=$(DEMO)) go run ./cmd/suite-seed

.PHONY: suite-smoke
suite-smoke: ## Sign in to all three and watch a chip follow Armature; outside the gate
	@[ -f $(SUITE_ENV_FILE) ] || { echo "The suite is not up. Start it with make suite-up ARMATURE_DIR=... STATOR_DIR=..."; exit 1; }
	@mkdir -p $(E2E_NPM_CACHE)
	$(DOCKER_PLAYWRIGHT) sh -c '$(E2E_INSTALL) && npx playwright test --config $(SUITE_SMOKE_CONFIG)'

.PHONY: suite-down
suite-down: ## Stop the suite, keeping its data: ARMATURE_DIR=<checkout> STATOR_DIR=<checkout>
	$(suite_dirs)
	$(SUITE_STATOR) down
	$(SUITE_ARMATURE) down
	$(SUITE_EXCITER) down

.PHONY: suite-clean
suite-clean: ## Stop the suite and forget its data: ARMATURE_DIR=<checkout> STATOR_DIR=<checkout>
	$(suite_dirs)
	$(SUITE_STATOR) down -v --remove-orphans
	$(SUITE_ARMATURE) --profile tools down -v --remove-orphans
	$(SUITE_EXCITER) down -v --remove-orphans
	@docker network rm $(SUITE_NET) >/dev/null 2>&1 || true
	@rm -f $(SUITE_ENV_FILE)
