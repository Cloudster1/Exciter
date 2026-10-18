# exciter-ci against this checkout's stack (#200), as a pipeline runs it against an installation. EXCITER_TOKEN is a
# token with the evidence scope and EXCITER_PROJECT the project's key; the reports are paths in this checkout.
CI_RUN = docker compose --profile tools run --rm -e EXCITER_TOKEN -e EXCITER_PROJECT ci

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
