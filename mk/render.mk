# The render service. Its tests run in the image it is built from, with the Chromium and puppeteer it prints with,
# against the script in the checkout rather than a built image.

RENDER_BASE_IMAGE := $(shell sed -n 's/^FROM //p' $(ROOT)/deploy/Dockerfile.render)

.PHONY: test-render
test-render: ## Run the render service's tests: refusals, the budget, the concurrency cap, the token and the origin
	@# Locked down as the chart runs it: the image's user by number, no capability, a root file system it cannot write.
	docker run --rm -e HOME=/tmp --user 1000:1000 --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges \
		-v $(ROOT)/render:/usr/src/app/render:ro -v $(ROOT)/tests/render:/tests:ro \
		--entrypoint node $(RENDER_BASE_IMAGE) --test /tests/server.test.mjs
