# The browser suite. It runs in Microsoft's Playwright image of exactly the version e2e/package.json pins, so the
# browsers in the image are the ones the library expects and nothing is downloaded at run time.
E2E_DIR := $(ROOT)/e2e
PLAYWRIGHT_VERSION := $(shell sed -n 's/.*"@playwright\/test": *"\([0-9][0-9.]*\)".*/\1/p' $(E2E_DIR)/package.json)
PLAYWRIGHT_IMAGE ?= mcr.microsoft.com/playwright:v$(PLAYWRIGHT_VERSION)-noble
E2E_NPM_CACHE := $(ROOT)/.cache/npm

WORKERS ?= 4
ONLY ?=
# Runs each chosen test this many times, to shake out a flake: REPEAT=20 ONLY=<grep>.
REPEAT ?=
# One part of the suite, as CI splits it across jobs: SHARD=2/4.
SHARD ?=
# DEBUG=pw:browser prints what Chromium writes to stderr, which names the failed check when a tab crashes.
DEBUG ?=
E2E_REPORT_PORT ?= 9323

# On the host's network rather than the stack's: the api trusts the session cookie only from
# http://localhost:$(WEB_PORT), and the sign-in sends the browser to Keycloak's published port, so the browser
# reaches the stack exactly as a person at this machine does. --ipc=host because Chromium runs out of the default
# 64 MB of shared memory. /tmp, where the browsers keep their profiles and Playwright its videos, is in memory: on
# the host's disk, a burst of writes from anything else froze every browser for seconds at a time.
DOCKER_PLAYWRIGHT = docker run --rm --init --ipc=host --network host --tmpfs /tmp:exec,mode=1777 $(DOCKER_NODE_TTY) \
	-u $(UID_GID) \
	-v $(ROOT):/src \
	-v $(E2E_NPM_CACHE):/npmcache \
	-e npm_config_cache=/npmcache \
	-e npm_config_update_notifier=false \
	-e HOME=/tmp \
	-e CI \
	-e DEBUG='$(DEBUG)' \
	-e ONLY='$(ONLY)' \
	-e REPEAT='$(REPEAT)' \
	-e SHARD='$(SHARD)' \
	-w /src/e2e $(PLAYWRIGHT_IMAGE)

# "Target crashed" says only that a tab died. These two say how: the kernel killing it for memory counts in the
# container's cgroup, while a crash of its own leaves a core dump with the signal on the host.
E2E_OOM_REPORT = k=$$(sed -n "s/^oom_kill //p" /sys/fs/cgroup/memory.events 2>/dev/null); \
	[ "$${k:-0}" -eq 0 ] || echo "The kernel killed $$k browser or test processes for lack of memory during this run. Run fewer gates at once or lower WORKERS."
E2E_CORE_REPORT = { command -v coredumpctl >/dev/null \
	&& coredumpctl --no-pager --since=@$$since list 2>/dev/null | grep -E "chrome|node" \
	&& echo "These browser or test processes crashed during this run; coredumpctl info <PID> shows the stack. Rerun with DEBUG=pw:browser to see Chromium's own message."; true; }

# The reach check, which test-e2e runs inline: a $(MAKE) on that recipe's line would make the `make -n test-e2e`
# run-tests.sh asks before the stack is up run the whole suite there, fail, and the layer be skipped as absent.
E2E_REACH = $(DOCKER_PLAYWRIGHT) node /src/scripts/e2e-reach.mjs /src/reports

# npm ci only when the lock file changed since the last install, which keeps a rerun of one spec to a couple of seconds.
E2E_INSTALL = [ node_modules/.package-lock.json -nt package-lock.json ] || npm ci --no-audit --no-fund

.PHONY: e2e-npm
e2e-npm: ## Run an npm command in the Playwright container: make e2e-npm ARGS="install"
	@mkdir -p $(E2E_NPM_CACHE)
	$(DOCKER_PLAYWRIGHT) npm $(ARGS)

.PHONY: test-e2e
test-e2e: ## Run the browser suite against this checkout's running stack: ONLY=<grep> WORKERS=<n> REPEAT=<n> SHARD=<i>/<n>
	@[ -f $(STACK_ENV_FILE) ] && docker compose ps --status running --services 2>/dev/null | grep -qx web \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	@mkdir -p $(E2E_NPM_CACHE)
	@since=$$(date +%s); \
	$(DOCKER_PLAYWRIGHT) sh -c '$(E2E_INSTALL) && npx tsc --noEmit && { npx playwright test --workers=$(WORKERS) $${ONLY:+--grep "$$ONLY"} $${REPEAT:+--repeat-each "$$REPEAT"} $${SHARD:+--shard "$$SHARD"}; rc=$$?; $(E2E_OOM_REPORT); exit $$rc; }'; \
	rc=$$?; [ $$rc -eq 0 ] || $(E2E_CORE_REPORT); [ $$rc -ne 0 ] || [ -n "$(ONLY)$(SHARD)$(REPEAT)" ] || $(E2E_REACH) || rc=$$?; exit $$rc

.PHONY: e2e-reach
e2e-reach: ## After a whole browser run: fail on a route never shown or a data-action never clicked that no exemption explains
	$(E2E_REACH)

.PHONY: e2e-report
e2e-report: ## Serve the last browser run's HTML report, traces included
	@echo "Report on http://localhost:$(E2E_REPORT_PORT)"
	$(DOCKER_PLAYWRIGHT) sh -c '$(E2E_INSTALL) && npx playwright show-report playwright-report --host 127.0.0.1 --port $(E2E_REPORT_PORT)'

# Where the report job of CI gathers the blobs of its browser jobs.
E2E_BLOBS ?= reports/e2e-blobs

.PHONY: e2e-merge-reports
e2e-merge-reports: ## Merge the blob reports in E2E_BLOBS into one HTML report and reports/e2e.json
	@mkdir -p $(E2E_NPM_CACHE)
	$(DOCKER_PLAYWRIGHT) sh -c '$(E2E_INSTALL) && npx playwright merge-reports --config merge.config.ts /src/$(E2E_BLOBS)'
