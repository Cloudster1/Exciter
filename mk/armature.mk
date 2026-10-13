# Armature's API document, vendored for the armature-stub's contract test.
# Updating it is how a change in Armature reaches Exciter: the test then says
# what the stub has to follow. Fetched in a container, like every tool here.
ARMATURE_REPO ?= Cloudster1/Armature
CURL_IMAGE ?= curlimages/curl:8.16.0
ARMATURE_API_DIR := $(ROOT)/api/armature

.PHONY: armature-openapi
armature-openapi: ## Vendor Armature's api/openapi.json at a commit: make armature-openapi REF=<sha or branch>
	@[ -n "$(REF)" ] || { echo "Name the Armature commit to vendor: make armature-openapi REF=<sha or branch>"; exit 1; }
	@mkdir -p $(ARMATURE_API_DIR)
	@sha=$$(docker run --rm $(CURL_IMAGE) -fsSL -H 'Accept: application/vnd.github.sha' \
		https://api.github.com/repos/$(ARMATURE_REPO)/commits/$(REF)) \
		|| { echo "GitHub does not know $(REF) in $(ARMATURE_REPO)."; exit 1; }; \
	docker run --rm $(CURL_IMAGE) -fsSL https://raw.githubusercontent.com/$(ARMATURE_REPO)/$$sha/api/openapi.json \
		> $(ARMATURE_API_DIR)/openapi.json.tmp \
		&& mv $(ARMATURE_API_DIR)/openapi.json.tmp $(ARMATURE_API_DIR)/openapi.json \
		&& printf '%s %s api/openapi.json\n' '$(ARMATURE_REPO)' "$$sha" > $(ARMATURE_API_DIR)/SOURCE \
		&& echo "Vendored $(ARMATURE_REPO) $$sha; run make check-go to see what the stub has to follow."
