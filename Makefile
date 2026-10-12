# Each area brings its own targets in mk/<area>.mk; run-tests.sh calls the gate targets
# (check-format, check-go, check-web, stack-up, test-integration, test-e2e, stack-down) when they exist.
.DEFAULT_GOAL := help

ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
VERSION := $(shell cat $(ROOT)/VERSION)
UID_GID := $(shell id -u):$(shell id -g)
# Machine-readable results of the last run of each suite, which scripts/test-summary.sh summarises.
REPORTS := $(ROOT)/reports

include $(wildcard $(ROOT)/mk/*.mk)

.PHONY: test-all
test-all: ## The whole gate, exactly as CI and the push hook run it
	$(ROOT)/run-tests.sh

.PHONY: test-shell
test-shell: ## The shell layer: release, CI and branch-guard tests
	$(ROOT)/run-tests.sh shell

.PHONY: help
help:
	@grep -hE '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | sort
