# Stator's API document, vendored for the stator-stub's contract test (#40), as Armature's is in armature.mk.
# Updating it is how a change in Stator reaches Exciter: the test then says what the stub has to follow.
STATOR_REPO ?= praetorianer777/Stator
STATOR_API_DIR := $(ROOT)/api/stator

.PHONY: stator-openapi
stator-openapi: ## Vendor Stator's api/openapi.json at a commit: make stator-openapi REF=<sha or branch>
	@[ -n "$(REF)" ] || { echo "Name the Stator commit to vendor: make stator-openapi REF=<sha or branch>"; exit 1; }
	@mkdir -p $(STATOR_API_DIR)
	@sha=$$(docker run --rm $(CURL_IMAGE) -fsSL -H 'Accept: application/vnd.github.sha' \
		https://api.github.com/repos/$(STATOR_REPO)/commits/$(REF)) \
		|| { echo "GitHub does not know $(REF) in $(STATOR_REPO)."; exit 1; }; \
	docker run --rm $(CURL_IMAGE) -fsSL https://raw.githubusercontent.com/$(STATOR_REPO)/$$sha/api/openapi.json \
		> $(STATOR_API_DIR)/openapi.json.tmp \
		&& mv $(STATOR_API_DIR)/openapi.json.tmp $(STATOR_API_DIR)/openapi.json \
		&& printf '%s %s api/openapi.json\n' '$(STATOR_REPO)' "$$sha" > $(STATOR_API_DIR)/SOURCE \
		&& echo "Vendored $(STATOR_REPO) $$sha; run make check-go to see what the stub has to follow."
