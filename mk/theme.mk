# The colour tokens Armature's and Stator's themes name (#59), vendored so a test holds Exciter's to theirs: a token
# renamed on any side fails here instead of a theme that quietly loses a colour on its way between the products.
THEME_TOKENS_FILE := web/src/lib/theme-tokens.ts

.PHONY: theme-tokens
theme-tokens: ## Vendor Armature's and Stator's theme token names: make theme-tokens ARMATURE_REF=<sha> STATOR_REF=<sha>
	@[ -n "$(ARMATURE_REF)" ] && [ -n "$(STATOR_REF)" ] || { echo "Name both commits: make theme-tokens ARMATURE_REF=<sha> STATOR_REF=<sha>"; exit 1; }
	@for pair in "$(ARMATURE_REPO) $(ARMATURE_REF) armature" "$(STATOR_REPO) $(STATOR_REF) stator"; do \
		set -- $$pair; \
		sha=$$(docker run --rm $(CURL_IMAGE) -fsSL -H 'Accept: application/vnd.github.sha' https://api.github.com/repos/$$1/commits/$$2) \
			|| { echo "GitHub does not know $$2 in $$1."; exit 1; }; \
		docker run --rm $(CURL_IMAGE) -fsSL https://raw.githubusercontent.com/$$1/$$sha/$(THEME_TOKENS_FILE) \
			| docker run --rm -i -v $(ROOT)/scripts:/scripts:ro $(NODE_IMAGE) node /scripts/theme-tokens.mjs "$$1 $$sha $(THEME_TOKENS_FILE)" \
			> $(ROOT)/api/$$3/theme-tokens.json.tmp \
			&& mv $(ROOT)/api/$$3/theme-tokens.json.tmp $(ROOT)/api/$$3/theme-tokens.json \
			&& echo "Vendored the theme tokens of $$1 $$sha."; \
	done
