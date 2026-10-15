# Installs the chart into a real cluster: kind makes one out of a container, so
# a machine needs only Docker. Heavy (it builds the backend and web images and
# starts about a dozen pods), so it is not part of ./run-tests.sh; CI runs it
# when something the chart installs changes. See scripts/chart-install-test.sh.
CHART_TOOLS_IMAGE ?= exciter-chart-tools:local

.PHONY: chart-tools
chart-tools: ## Build the image with kind, helm and kubectl the install test runs in
	docker build -q -t $(CHART_TOOLS_IMAGE) $(ROOT)/scripts/kind >/dev/null

.PHONY: test-chart-install
test-chart-install: chart-tools ## Install and upgrade the chart in a throwaway kind cluster
	docker run --rm --network host \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(ROOT):/work:ro \
		-e EXCITER_BUILD_VERSION -e EXCITER_BUILD_COMMIT \
		-e KEEP_CLUSTER -e CLUSTER=exciter-chart-install-$$(printf '%s' $(ROOT) | cksum | cut -d' ' -f1) \
		$(CHART_TOOLS_IMAGE) /work/scripts/chart-install-test.sh
