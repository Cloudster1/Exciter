# exciter-ci against this checkout's stack (#200), as a pipeline runs it against an installation. EXCITER_TOKEN is a
# token with the evidence scope and EXCITER_PROJECT the project's key; the reports are paths in this checkout.
CI_RUN = docker compose --profile tools run --build --rm -e EXCITER_TOKEN -e EXCITER_PROJECT ci

.PHONY: ci-image
ci-image: ## Build the exciter-ci image a pipeline runs
	docker compose --profile tools build ci

.PHONY: test-results
test-results: ## Upload JUnit reports to the stack: make test-results REPORTS_GLOB='reports/*.xml' COMMIT=<sha> BRANCH=main
	@test -n "$(REPORTS_GLOB)" || { echo "Name the reports with REPORTS_GLOB, a path in this checkout."; exit 2; }
	$(CI_RUN) results --repository "$(or $(REPOSITORY),exciter/local)" --commit "$(or $(COMMIT),$(shell git rev-parse HEAD))" \
		--branch "$(or $(BRANCH),$(shell git rev-parse --abbrev-ref HEAD))" '$(REPORTS_GLOB)'

.PHONY: trace-check
trace-check: ## Ask the stack's trace check for a verdict: make trace-check [BRANCH=main] [QUERY='type = Feature'] [FAIL=failing]
	$(CI_RUN) check $(if $(BRANCH),--branch "$(BRANCH)") $(if $(QUERY),--query "$(QUERY)") $(if $(FAIL),--fail "$(FAIL)")

# A checkout mounted from the host belongs to another user than the container's, which git refuses to read unless told.
GIT_SAFE = -e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory -e GIT_CONFIG_VALUE_0='*'

.PHONY: code-scan
code-scan: ## Upload the requirement tags of this checkout, or of DIR inside it, to the stack: make code-scan [DIR=backend] [BRANCH=main] [COMMIT=<sha>]
	docker compose --profile tools run --build --rm -e EXCITER_TOKEN -e EXCITER_PROJECT $(GIT_SAFE) ci scan \
		$(if $(BRANCH),--branch "$(BRANCH)") $(if $(COMMIT),--commit "$(COMMIT)") $(if $(REPOSITORY),--repository "$(REPOSITORY)") $(or $(DIR),.)

# The integration layer runs it after the suite, so the image and the targets above are tried as a pipeline runs them.
.PHONY: test-exciter-ci
test-exciter-ci: ## Run the ci image through the targets above against this checkout's running stack
	$(ROOT)/tests/exciter-ci-stack.sh
