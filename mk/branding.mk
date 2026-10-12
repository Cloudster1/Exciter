# The logo and icons in web/public, made from assets/branding/logo.jpg.
# ImageMagick runs from an image, as every other tool does, and as root only
# for the package install; the files are handed back to the caller after.
BRANDING_IMAGE ?= alpine:3.22

.PHONY: branding
branding: ## Make web/public's logo and icons from assets/branding/logo.jpg
	docker run --rm -v $(ROOT):/code $(BRANDING_IMAGE) sh -ec \
		'apk add --no-cache imagemagick imagemagick-jpeg imagemagick-webp >/dev/null && /code/scripts/branding.sh && chown -R $(UID_GID) /code/web/public'
