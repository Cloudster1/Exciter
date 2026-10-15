#!/usr/bin/env bash
# Tests for deploy/charts/exciter: lint and template for each database layout, the partners and the bootstrap, what
# the chart must refuse, and the golden renders in tests/helm (UPDATE=1 rewrites them). Helm runs in a container.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
CHART="${SRC}/deploy/charts/exciter"
GOLDEN="${SRC}/tests/helm"
HELM_IMAGE="${HELM_IMAGE:-alpine/helm:3.19.0}"
RELEASE=r
FULL="${RELEASE}-exciter"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

helm() {
    docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -v "${CHART}:/chart:ro" "${HELM_IMAGE}" "$@"
}

# The one value the api, worker and Jobs are given for an environment variable; more than one means two workloads
# disagree. The migrate Job's owner URL is checked on its own by migrate_url.
env_value() { # rendered name
    grep -A1 -- "- name: $2\$" <<<"$1" | sed -n 's/^ *value: "\(.*\)"$/\1/p' | grep -vF 'DB_OWNER_PASSWORD' | sort -u | paste -sd ' '
}

# A setting the chart puts in the shared ConfigMap, as KEY: "value".
config_value() { # rendered name
    sed -n "s/^ *$2: \"\(.*\)\"\$/\1/p" <<<"$1" | sort -u | paste -sd ' '
}

migrate_url() { # rendered
    grep -A1 -- '- name: EXCITER_DB_PRIMARY_URL$' <<<"$1" | grep -F DB_OWNER_PASSWORD | sed -n 's/^ *value: "\(.*\)"$/\1/p' | sort -u
}

# lint and template with the same settings, which must both succeed.
render() { # description args...
    local what="$1"
    shift
    if out=$(helm lint /chart --strict "$@" 2>&1); then
        pass "${what}: helm lint"
    else
        fail "${what}: helm lint"
        echo "${out}" | sed 's/^/      /'
    fi
    if RENDERED=$(helm template "${RELEASE}" /chart "$@" 2>&1); then
        pass "${what}: helm template"
        # Only the compose stack trades commit durability for speed.
        check "${what}: commits wait for the disk" "$(grep -c 'synchronous_commit' <<<"${RENDERED}")" "0"
        # The stubs stand in for the partners in the test stacks alone.
        check "${what}: no partner stubs" "$(grep -cE 'armature-stub|stator-stub' <<<"${RENDERED}")" "0"
    else
        fail "${what}: helm template"
        echo "${RENDERED}" | sed 's/^/      /'
        RENDERED=""
    fi
}

refused() { # description expected-message args...
    local what="$1" message="$2"
    shift 2
    if out=$(helm template "${RELEASE}" /chart "$@" 2>&1); then
        fail "${what}: rendered, but should have been refused"
    elif grep -qF -- "${message}" <<<"${out}"; then
        pass "${what}"
    else
        fail "${what}: refused without saying [${message}]"
        echo "${out}" | sed 's/^/      /'
    fi
}

# A render compared with its golden file; UPDATE=1 writes the file instead.
golden() { # name args...
    local name="$1" file="${GOLDEN}/$1.yaml"
    shift
    local out
    if ! out=$(helm template "${RELEASE}" /chart "$@" 2>&1); then
        fail "golden ${name}: helm template"
        echo "${out}" | sed 's/^/      /'
        return
    fi
    if [[ "${UPDATE:-}" == 1 ]]; then
        printf '%s\n' "${out}" >"${file}"
        pass "golden ${name}: written"
    elif [[ ! -f "${file}" ]]; then
        fail "golden ${name}: ${file} is missing; run UPDATE=1 tests/test-helm.sh and review it"
    elif diff -u "${file}" <(printf '%s\n' "${out}") >"${GOLDEN}/.diff" 2>&1; then
        pass "golden ${name}: the render is unchanged"
    else
        fail "golden ${name}: the render differs; review it, then run UPDATE=1 tests/test-helm.sh"
        head -40 "${GOLDEN}/.diff" | sed 's/^/      /'
    fi
    rm -f "${GOLDEN}/.diff"
}

app_url() { echo "postgres://exciter_app:\$(DB_APP_PASSWORD)@$1:5432/exciter?sslmode=$2"; }
admin_url() { echo "postgres://exciter_admin:\$(DB_ADMIN_PASSWORD)@$1:5432/exciter?sslmode=$2"; }
owner_url() { echo "postgres://$1:\$(DB_OWNER_PASSWORD)@$2:5432/exciter?sslmode=$3"; }

RW="${FULL}-postgres-rw"
RO="${FULL}-postgres-ro"
VALKEY=(--set valkey.host=valkey.example)

echo "== CloudNativePG with three instances"
render "cnpg, 3 instances" --set cnpg.enabled=true "${VALKEY[@]}"
check "writes go to -rw" "$(env_value "${RENDERED}" EXCITER_DB_PRIMARY_URL)" "$(app_url "${RW}" require)"
check "admin work goes to -rw" "$(env_value "${RENDERED}" EXCITER_DB_ADMIN_URL)" "$(admin_url "${RW}" require)"
check "reads go to -ro" "$(env_value "${RENDERED}" EXCITER_DB_REPLICA_URLS)" "$(app_url "${RO}" require)"
check "migrations run as the owner on -rw" "$(migrate_url "${RENDERED}")" "$(owner_url exciter_owner "${RW}" require)"
check "the Cluster is rendered with three instances" "$(grep -c '^kind: Cluster$' <<<"${RENDERED}") $(grep -c '^  instances: 3$' <<<"${RENDERED}")" "1 1"
check "the operator makes both runtime roles" "$(grep -cE '^    - (name: exciter_app|name: exciter_admin)|^      name: exciter_(app|admin)$' <<<"${RENDERED}")" "2"
check "the Valkey URL reaches every workload" "$(env_value "${RENDERED}" EXCITER_VALKEY_URL)" "redis://valkey.example:6379/0"
check "the images are the ones a release pushes, at the chart's appVersion" \
    "$(grep -oE 'image: ghcr.io/cloudster1/exciter-(backend|web):[^ ]+' <<<"${RENDERED}" | sort -u | paste -sd ' ')" \
    "image: ghcr.io/cloudster1/exciter-backend:$(sed -n 's/^appVersion: "\(.*\)"$/\1/p' "${CHART}/Chart.yaml") image: ghcr.io/cloudster1/exciter-web:$(sed -n 's/^appVersion: "\(.*\)"$/\1/p' "${CHART}/Chart.yaml")"
check "the upload limit is set, and can be changed" \
    "$(config_value "${RENDERED}" EXCITER_UPLOAD_LIMIT) $(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set attachments.uploadLimit=2GB 2>&1 | grep -c 'EXCITER_UPLOAD_LIMIT: "2GB"')" \
    "50MB 1"
check "the audit log is kept a year, and that can be changed" \
    "$(config_value "${RENDERED}" EXCITER_RETAIN_AUDIT) $(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set retention.audit=2160h 2>&1 | grep -c 'EXCITER_RETAIN_AUDIT: "2160h"')" \
    "8760h 1"
check "mail is off until a relay is named" "$(grep -c 'EXCITER_SMTP_ADDR' <<<"${RENDERED}")" "0"
check "PDF export is off until the render service is enabled" "$(grep -cE 'EXCITER_RENDER_URL|component: render' <<<"${RENDERED}")" "0"
with_render=$(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set render.enabled=true 2>&1)
check "enabled, the render service runs beside the web and the api prints through it" \
    "$(config_value "${with_render}" EXCITER_RENDER_URL) $(grep -c "^  name: ${RELEASE}-exciter-render\$" <<<"${with_render}") $(grep -c 'name: RENDER_APP_URL' <<<"${with_render}")" \
    "http://${RELEASE}-exciter-render:8090 2 1"
check "its image is the one a release pushes" "$(grep -c 'image: ghcr.io/cloudster1/exciter-render:' <<<"${with_render}")" "1"
check "the installation keeps the name Exciter until one is given" \
    "$(grep -c 'EXCITER_NAME' <<<"${RENDERED}") $(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set installation.name=Acme 2>&1 | grep -c 'EXCITER_NAME: "Acme"')" \
    "0 1"

echo "== The partners and the identity provider, reached inside the cluster"
check "nothing inside the network is reached until named" "$(grep -cE 'EXCITER_(OUTBOUND_ALLOW|ARMATURE_BACKCHANNEL|STATOR_BACKCHANNEL|OIDC_BACKCHANNEL)' <<<"${RENDERED}")" "0"
WIRED=(--set cnpg.enabled=true "${VALKEY[@]}" --set jobs.bootstrap.enabled=true
    --set armature.url=https://armature.example.com --set stator.url=https://wiki.example.com
    --set 'armature.backchannel.https://armature\.example\.com=http://armature-api.armature.svc:8080'
    --set 'stator.backchannel.https://wiki\.example\.com=http://stator-api.stator.svc:8080'
    --set 'auth.oidcBackchannel.https://sso\.example\.com=http://keycloak.sso.svc:8080'
    --set 'network.outboundAllow={10.40.0.0/16}')
render "partners wired" "${WIRED[@]}"
check "each partner is reached where it is mapped" \
    "$(grep -E 'EXCITER_(ARMATURE|STATOR|OIDC)_BACKCHANNEL' <<<"${RENDERED}" | tr -d ' ' | sort | paste -sd ' ')" \
    'EXCITER_ARMATURE_BACKCHANNEL:"https://armature.example.com=http://armature-api.armature.svc:8080" EXCITER_OIDC_BACKCHANNEL:"https://sso.example.com=http://keycloak.sso.svc:8080" EXCITER_STATOR_BACKCHANNEL:"https://wiki.example.com=http://stator-api.stator.svc:8080"'
check "the guard lets through what is named and every host a backchannel reaches" \
    "$(config_value "${RENDERED}" EXCITER_OUTBOUND_ALLOW)" \
    "10.40.0.0/16,armature-api.armature.svc,stator-api.stator.svc,keycloak.sso.svc"
check "the bootstrap connects the partners at the addresses people open" \
    "$(env_value "${RENDERED}" EXCITER_BOOTSTRAP_ARMATURE_URL) $(env_value "${RENDERED}" EXCITER_BOOTSTRAP_STATOR_URL)" \
    "https://armature.example.com https://wiki.example.com"

echo "== One api pod without Valkey"
render "one api pod, no Valkey" --set database.host=db.example --set api.replicas=1
check "no Valkey URL" "$(env_value "${RENDERED}" EXCITER_VALKEY_URL)" ""

echo "== CloudNativePG with one instance"
render "cnpg, 1 instance" --set cnpg.enabled=true --set cnpg.spec.instances=1 "${VALKEY[@]}"
check "no replica URL, so reads go to -rw" "$(env_value "${RENDERED}" EXCITER_DB_REPLICA_URLS)" ""
check "-ro is named nowhere" "$(grep -c -- "${RO}" <<<"${RENDERED}")" "0"

echo "== A database of one's own"
render "cnpg off" --set database.host=db.example --set "database.replicaHosts={db-ro.example}" "${VALKEY[@]}"
check "writes go to database.host" "$(env_value "${RENDERED}" EXCITER_DB_PRIMARY_URL)" "$(app_url db.example require)"
check "reads go to database.replicaHosts" "$(env_value "${RENDERED}" EXCITER_DB_REPLICA_URLS)" "$(app_url db-ro.example require)"
check "migrations run as postgres there" "$(migrate_url "${RENDERED}")" "$(owner_url postgres db.example require)"
check "no Cluster is rendered" "$(grep -c '^kind: Cluster$' <<<"${RENDERED}")" "0"

echo "== The demo values, with the bundled Postgres, Valkey and SeaweedFS"
render "demo" -f /chart/values-demo.yaml
check "writes go to the bundled Postgres" "$(env_value "${RENDERED}" EXCITER_DB_PRIMARY_URL)" "$(app_url "${FULL}-postgresql" disable)"
check "the bundled Valkey is used with its password" "$(env_value "${RENDERED}" EXCITER_VALKEY_URL)" "redis://:\$(VALKEY_PASSWORD)@${FULL}-valkey:6379/0"
check "the api is pointed at the S3 service of the release" "$(config_value "${RENDERED}" EXCITER_S3_ENDPOINT)" "${RELEASE}-seaweedfs-s3:8333"
check "the bootstrap makes the administrator" "$(grep -c 'command: \["/app/seed", "bootstrap"\]' <<<"${RENDERED}")" "1"

echo "== What the chart refuses"
refused "cnpg and the bundled Postgres together" \
    "cnpg.enabled and postgresql.enabled are both true. Turn one of them off" \
    --set cnpg.enabled=true --set postgresql.enabled=true "${VALKEY[@]}"
refused "replicas behind several api pods without Valkey" \
    "read-your-writes needs a Valkey all api pods share" \
    --set cnpg.enabled=true
refused "no database at all" "Set database.host, or enable cnpg or the bundled postgresql" \
    --set database.host= "${VALKEY[@]}"
refused "postgres as the owner under cnpg" "database.ownerRole is postgres, which CNPG keeps for its own superuser" \
    --set cnpg.enabled=true --set database.ownerRole=postgres "${VALKEY[@]}"
refused "the seed in production" "jobs.seed.enabled is true but env is production" \
    --set database.host=db.example --set jobs.seed.enabled=true "${VALKEY[@]}"
refused "partners nobody connects" "armature.url and stator.url are connected by the bootstrap Job" \
    --set database.host=db.example "${VALKEY[@]}" --set armature.url=https://armature.example.com

BOOT=(--set database.host=db.example "${VALKEY[@]}" --set jobs.bootstrap.enabled=true
    --set bootstrap.admin.email=root@acme.example --set bootstrap.admin.passwordSecret.name=acme-admin)

echo "== The bootstrap Job in production"
render "bootstrap" "${BOOT[@]}"
check "it runs seed bootstrap" "$(grep -c 'command: \["/app/seed", "bootstrap"\]' <<<"${RENDERED}")" "1"
check "the password is read from its Secret, not written" "$(grep -A4 'name: EXCITER_BOOTSTRAP_ADMIN_PASSWORD$' <<<"${RENDERED}" | grep -c 'name: "acme-admin"')" "1"
check "no provider unless named" "$(grep -c 'EXCITER_BOOTSTRAP_OIDC' <<<"${RENDERED}")" "0"
check "no partner unless named" "$(grep -cE 'EXCITER_BOOTSTRAP_(ARMATURE|STATOR)_URL' <<<"${RENDERED}")" "0"
render "bootstrap with the suite's realm and members" "${BOOT[@]}" \
    --set bootstrap.oidc.issuer=https://sso.example/realms/dynamo --set bootstrap.oidc.clientId=exciter \
    --set bootstrap.oidc.clientSecret.name=dynamo-oidc \
    --set "bootstrap.members[0].email=a@acme.example" --set "bootstrap.members[0].role=admin"
check "the realm is the issuer" "$(env_value "${RENDERED}" EXCITER_BOOTSTRAP_OIDC_ISSUER)" "https://sso.example/realms/dynamo"
check "the provider's client secret is read from its Secret" "$(grep -A4 'name: EXCITER_BOOTSTRAP_OIDC_CLIENT_SECRET$' <<<"${RENDERED}" | grep -c 'name: "dynamo-oidc"')" "1"
check "members are let in" "$(env_value "${RENDERED}" EXCITER_BOOTSTRAP_MEMBERS)" "a@acme.example=admin"
render "bootstrap with nothing but the switch" --set database.host=db.example "${VALKEY[@]}" --set jobs.bootstrap.enabled=true
check "the administrator is admin@ the chart's host" "$(env_value "${RENDERED}" EXCITER_BOOTSTRAP_ADMIN_EMAIL)" "admin@exciter.example.com"
check "the password is read from the generated Secret" "$(grep -A4 'name: EXCITER_BOOTSTRAP_ADMIN_PASSWORD$' <<<"${RENDERED}" | grep -c 'name: "r-exciter-bootstrap-admin"')" "1"
check "the secrets Job makes that Secret" "$(grep -c 'if exists "r-exciter-bootstrap-admin"' <<<"${RENDERED}")" "1"
refused "the bootstrap with nobody to make the password" "nothing can make the administrator's password" \
    --set database.host=db.example "${VALKEY[@]}" --set jobs.bootstrap.enabled=true --set secrets.generate=false
refused "a provider without its client id" "set bootstrap.oidc.clientId" \
    "${BOOT[@]}" --set bootstrap.oidc.issuer=https://sso.example
refused "a member with no such role" "use owner, admin or member" \
    "${BOOT[@]}" --set "bootstrap.members[0].email=a@acme.example" --set "bootstrap.members[0].role=root"
refused "the seed and the bootstrap together" "turn one of them off" \
    "${BOOT[@]}" --set jobs.seed.enabled=true --set env=development

echo "== The bundled SeaweedFS"
SWFS=(--set database.host=db.example "${VALKEY[@]}" --set seaweedfs.enabled=true)
render "bundled SeaweedFS" "${SWFS[@]}"
check "without TLS inside the release" "$(config_value "${RENDERED}" EXCITER_S3_USE_SSL)" "false"
check "the api and worker read the keys from the generated Secret" "$(grep -A4 'name: EXCITER_S3_ACCESS_KEY$' <<<"${RENDERED}" | grep -c 'name: "\?exciter-s3"\?$')" "2"
render "an S3 of one's own" --set database.host=db.example "${VALKEY[@]}" --set s3.enabled=true --set s3.endpoint=s3.example --set secrets.s3AccessKey=key --set secrets.s3SecretKey=secret
check "the endpoint is the one given" "$(config_value "${RENDERED}" EXCITER_S3_ENDPOINT)" "s3.example"
check "no SeaweedFS is rendered" "$(grep -c 'seaweedfs' <<<"${RENDERED}")" "0"
refused "SeaweedFS reading keys the api does not have" "must be \"exciter-s3\" and \"exciter-s3-read\"" \
    "${SWFS[@]}" --set seaweedfs.s3.credentials.admin.existingSecret=elsewhere

echo "== The golden renders"
golden demo -f /chart/values-demo.yaml
golden production "${WIRED[@]}" --set bootstrap.oidc.issuer=https://sso.example.com/realms/dynamo \
    --set bootstrap.oidc.clientId=exciter --set bootstrap.oidc.clientSecret.name=dynamo-oidc

if [[ ${FAILED} -ne 0 ]]; then
    echo "chart tests failed"
    exit 1
fi
echo "chart tests passed"
