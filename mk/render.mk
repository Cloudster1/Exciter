# The render service. Its tests run in the image it is built from, with the Chromium and puppeteer it prints with,
# against the script in the checkout rather than a built image.

RENDER_BASE_IMAGE := $(shell sed -n 's/^FROM //p' $(ROOT)/deploy/Dockerfile.render)

.PHONY: test-render
test-render: ## Run the render service's tests: refusals, the budget, the concurrency cap, the token and the origin
	docker run --rm -e HOME=/tmp \
		-v $(ROOT)/render:/usr/src/app/render:ro -v $(ROOT)/tests/render:/tests:ro \
		--entrypoint node $(RENDER_BASE_IMAGE) --test /tests/server.test.mjs
