# ReqIF exports held to the ReqIF 1.2 schema (#44). xmllint runs from an image, as every other tool does, and as
# root only for the package install; the fetched schema is kept in the Go cache, which CI keeps too.
REQIF_IMAGE ?= alpine:3.22

# The documents the schema checks: the export reqifio's golden test writes, and the file its tests read.
REQIF_DOCUMENTS := backend/internal/reqifio/testdata/export.reqif backend/internal/reqifio/testdata/supplier.reqif

.PHONY: reqif-schema
reqif-schema: | $(GO_CACHE) ## Check the golden ReqIF export and the test file against the ReqIF 1.2 schema
	docker run --rm -v $(ROOT):/code:ro -v $(GO_CACHE):/cache $(REQIF_IMAGE) sh -ec \
		'apk add --no-cache curl libxml2-utils >/dev/null; status=0; sh /code/scripts/reqif-schema.sh $(addprefix /code/,$(REQIF_DOCUMENTS)) || status=$$?; chown -R $(UID_GID) /cache/reqif-schema; exit $$status'

.PHONY: reqif-golden
reqif-golden: | $(GO_CACHE) ## Write reqifio's golden export again after a change to what an export writes
	$(DOCKER_GO) sh -c 'REQIF_GOLDEN_UPDATE=1 go test ./internal/reqifio -run TestTheGoldenExportIsCurrent'
