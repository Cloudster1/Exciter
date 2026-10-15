#!/bin/sh
# Checks ReqIF documents against the ReqIF 1.2 schema, inside the container make reqif-schema starts. The files name
# each other by URL, so they are fetched once, held to api/reqif/schema.sha256, and read offline through a catalog.
set -eu
set -o pipefail

MANIFEST=/code/api/reqif/schema.sha256
SCHEMA=/cache/reqif-schema

grep -v '^#' "$MANIFEST" | while read -r sum url; do
  file="$SCHEMA/${url#https://}"
  if [ -f "$file" ] && echo "$sum  $file" | sha256sum -c -s; then
    continue
  fi
  mkdir -p "$(dirname "$file")"
  curl -fsSL -A exciter-reqif-schema -o "$file" "$url"
  if ! echo "$sum  $file" | sha256sum -c -s; then
    echo "$url changed since api/reqif/schema.sha256 was written. Check what changed, and update its checksum there." >&2
    rm -f "$file"
    exit 1
  fi
done

cat > "$SCHEMA/catalog.xml" <<EOF
<?xml version="1.0"?>
<catalog xmlns="urn:oasis:names:tc:entity:xmlns:xml:catalog">
  <rewriteURI uriStartString="http://www.w3.org/" rewritePrefix="file://$SCHEMA/www.w3.org/"/>
  <rewriteURI uriStartString="https://www.w3.org/" rewritePrefix="file://$SCHEMA/www.w3.org/"/>
  <rewriteURI uriStartString="http://www.omg.org/" rewritePrefix="file://$SCHEMA/www.omg.org/"/>
  <rewriteURI uriStartString="https://www.omg.org/" rewritePrefix="file://$SCHEMA/www.omg.org/"/>
</catalog>
EOF

# The modules import the XHTML datatypes by two locations; xmllint warns once per document that it skips one.
XML_CATALOG_FILES="$SCHEMA/catalog.xml" xmllint --noout --nonet \
  --schema "$SCHEMA/www.omg.org/spec/ReqIF/20110401/reqif.xsd" "$@" 2>&1 | grep -v 'Skipping import of schema'
