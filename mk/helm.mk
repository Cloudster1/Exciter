# The Helm chart. Helm runs in a container like every other toolchain.
HELM_IMAGE ?= alpine/helm:3.19.0
export HELM_IMAGE

.PHONY: test-helm
test-helm: ## Lint and render deploy/charts/exciter for each database layout
	$(ROOT)/tests/test-helm.sh

# The one subchart is fetched into the chart, and what it fetched is committed:
# see Chart.yaml for why. The chart directory is mounted writable for this.
.PHONY: helm-deps
helm-deps: ## Refresh the SeaweedFS subchart archive and Chart.lock after a change to Chart.yaml
	docker run --rm -u $(UID_GID) -e HOME=/tmp -v $(ROOT)/deploy/charts/exciter:/chart -w /chart $(HELM_IMAGE) dependency update

# What a release publishes, exciter-<version>.tgz, packed from the committed
# subchart archive, so it needs no network.
CHART_DIST ?= $(ROOT)/dist
CHART_VERSION = $(shell sed -n 's/^version: //p' $(ROOT)/deploy/charts/exciter/Chart.yaml)

.PHONY: chart-package
chart-package: ## Package deploy/charts/exciter into dist/ as exciter-<version>.tgz
	mkdir -p $(CHART_DIST)
	docker run --rm -u $(UID_GID) -e HOME=/tmp -v $(ROOT)/deploy/charts/exciter:/chart:ro -v $(CHART_DIST):/dist $(HELM_IMAGE) package /chart --destination /dist

# release.yml sets all three, CHART_REPOSITORY next to the image names.
.PHONY: chart-push
chart-push: ## Push dist/exciter-<version>.tgz to CHART_REPOSITORY (release.yml does this)
	@test -n "$$CHART_REPOSITORY" -a -n "$$REGISTRY_USER" -a -n "$$REGISTRY_TOKEN" \
		|| { echo "Set CHART_REPOSITORY (oci://host/path), REGISTRY_USER and REGISTRY_TOKEN; release.yml sets all three." >&2; exit 1; }
	docker run --rm -u $(UID_GID) -e HOME=/tmp -e CHART_REPOSITORY -e REGISTRY_USER -e REGISTRY_TOKEN \
		-v $(CHART_DIST):/dist:ro --entrypoint sh $(HELM_IMAGE) -c \
		'host=$${CHART_REPOSITORY#oci://}; host=$${host%%/*}; printf %s "$$REGISTRY_TOKEN" | helm registry login "$$host" -u "$$REGISTRY_USER" --password-stdin && helm push /dist/exciter-$(CHART_VERSION).tgz "$$CHART_REPOSITORY"'
