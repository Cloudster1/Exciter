# The GitHub workflows, checked by actionlint and the shellcheck it carries. The release workflow runs only for a
# release, so this is where a mistake in it shows before one.
ACTIONLINT_IMAGE ?= rhysd/actionlint:1.7.7

.PHONY: check-actions
check-actions: ## Lint .github/workflows with actionlint
	docker run --rm -u $(UID_GID) -v $(ROOT):/repo:ro -w /repo $(ACTIONLINT_IMAGE) -no-color
