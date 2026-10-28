#!/usr/bin/env bash
# Installs deploy/charts/exciter into a throwaway kind cluster and checks what a render cannot: the hook Jobs, ready
# workloads, sign-in, connected partners, the work the worker and the bucket do, a PDF printed by the render service,
# and an upgrade without a failed health check. Run by make test-chart-install.
set -euo pipefail

ROOT="${ROOT:-/work}"
CLUSTER="${CLUSTER:-exciter-chart-install}"
NODE_IMAGE="${NODE_IMAGE:-kindest/node:v1.34.0}"
RELEASE=trial
NS=exciter
FULL="${RELEASE}-exciter"
LOCAL_PORT="${LOCAL_PORT:-18080}"
# How long the worker may take to finish an import, and to drain the outbox after it.
WORKER_WAIT="${WORKER_WAIT:-180}"
# How long one PDF may take to print, the first start of the browser included.
PDF_WAIT="${PDF_WAIT:-120}"
KUBECONFIG="$(mktemp)"
export KUBECONFIG
FORWARD_PID=""

say() { printf '\n== %s\n' "$*"; }
fail() { printf '   FAIL  %s\n' "$*"; exit 1; }
ok() { printf '   ok    %s\n' "$*"; }

dump() {
  say "What the cluster says"
  kubectl get pods,jobs,svc -n "$NS" -o wide 2>&1 || true
  for pod in $(kubectl get pods -n "$NS" -o name 2>/dev/null); do
    phase=$(kubectl get "$pod" -n "$NS" -o jsonpath='{.status.phase}' 2>/dev/null || true)
    if [[ "$phase" != "Running" && "$phase" != "Succeeded" ]] || ! kubectl get "$pod" -n "$NS" -o jsonpath='{.status.containerStatuses[*].ready}' 2>/dev/null | grep -qv false; then
      echo "--- $pod"
      kubectl describe "$pod" -n "$NS" 2>&1 | tail -25 || true
      kubectl logs "$pod" -n "$NS" --all-containers --tail=40 2>&1 || true
    fi
  done
}

cleanup() {
  local code=$?
  [[ $code -ne 0 ]] && dump
  [[ -n "$FORWARD_PID" ]] && kill "$FORWARD_PID" 2>/dev/null || true
  if [[ "${KEEP_CLUSTER:-}" != 1 ]]; then
    kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
  else
    echo "cluster $CLUSTER kept; kind export kubeconfig --name $CLUSTER"
  fi
  rm -f "$KUBECONFIG"
  exit "$code"
}
trap cleanup EXIT

# kube-proxy needs the mark target for service addresses; without it every pod
# times out on the API server and the first Job fails after a long wait.
if [[ -r /proc/modules ]] && ! grep -q '^xt_mark ' /proc/modules && ! grep -q '^xt_MARK ' /proc/modules; then
  echo "note: the xt_mark kernel module is not loaded; if pods cannot reach services, run: sudo modprobe xt_mark xt_MARK"
fi

say "A cluster"
kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
kind create cluster --name "$CLUSTER" --image "$NODE_IMAGE" --kubeconfig "$KUBECONFIG" --wait 180s
ok "kind cluster $CLUSTER is up"

say "The images, built from this checkout and loaded into the cluster"
# make passes the stamp in: the tools image has no git to read it with.
BUILD_VERSION="${EXCITER_BUILD_VERSION:-$("$ROOT/scripts/build-stamp.sh" version)}"
BUILD_COMMIT="${EXCITER_BUILD_COMMIT:-$("$ROOT/scripts/build-stamp.sh" commit)}"
for image in backend web render; do
  docker build -q -f "$ROOT/deploy/Dockerfile.$image" -t "exciter-test/$image:ci" \
    --build-arg VERSION="$BUILD_VERSION" --build-arg COMMIT="$BUILD_COMMIT" "$ROOT" >/dev/null
done
kind load docker-image --name "$CLUSTER" exciter-test/backend:ci exciter-test/web:ci exciter-test/render:ci
ok "backend, web and render are on the node"

say "Install"
kubectl create namespace "$NS"
# Not --wait: Helm then waits for the workloads before it runs post-install
# hooks, and the api and worker cannot be ready until the roles and migrate
# Jobs, which are those hooks, have run. Helm blocks on the hooks either way;
# the workloads are waited for below.
helm install "$RELEASE" "$ROOT/deploy/charts/exciter" -n "$NS" -f "$ROOT/scripts/chart-install-values.yaml" --timeout 15m
ok "helm ran the hook Jobs"

for job in roles secrets-generator migrate bootstrap; do
  name="$(kubectl get jobs -n "$NS" -o name | grep -- "-$job" | head -1)"
  [[ -n "$name" ]] || fail "no $job Job was run"
  [[ "$(kubectl get "$name" -n "$NS" -o jsonpath='{.status.succeeded}')" == 1 ]] || fail "the $job Job did not succeed"
done
ok "the roles, secrets, migrate and bootstrap Jobs all succeeded"

# The api and worker start before the hooks have made the database and crash
# until they have, so kubelet's back-off, up to five minutes, is in this wait.
for workload in api worker web kroki render; do
  kubectl rollout status "deploy/${FULL}-${workload}" -n "$NS" --timeout=360s >/dev/null || fail "$workload is not ready"
done
ok "api, worker, web, the diagram renderer and the render service are ready"

say "Through the web server"
kubectl port-forward -n "$NS" "svc/${FULL}-web" "${LOCAL_PORT}:80" >/dev/null 2>&1 &
FORWARD_PID=$!
for _ in $(seq 1 30); do
  curl -fs "http://127.0.0.1:${LOCAL_PORT}/healthz" >/dev/null 2>&1 && break
  sleep 1
done
[[ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${LOCAL_PORT}/healthz")" == 200 ]] || fail "/healthz through the web server is not 200"
[[ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${LOCAL_PORT}/readyz")" == 200 ]] || fail "/readyz through the web server is not 200"
ok "the web server reaches the api, and the api reaches its database"
meta="$(curl -fs "http://127.0.0.1:${LOCAL_PORT}/api/v1/meta")" || fail "/api/v1/meta through the web server did not answer"
[[ "$(jq -r '.product + " " + .build.version + " " + (.build.commit // "")' <<<"$meta")" == "exciter $BUILD_VERSION $BUILD_COMMIT" ]] \
  || fail "/api/v1/meta says $meta, want exciter $BUILD_VERSION $BUILD_COMMIT"
ok "the api reports the build it was stamped with, to anybody"

PASSWORD="$(kubectl get secret "${FULL}-bootstrap-admin" -n "$NS" -o jsonpath='{.data.password}' | base64 -d)"
JAR="$(mktemp)"
code="$(curl -s -o /dev/null -w '%{http_code}' -c "$JAR" -H 'Content-Type: application/json' \
  -d "{\"email\":\"admin@chart-test.example\",\"password\":\"${PASSWORD}\"}" "http://127.0.0.1:${LOCAL_PORT}/api/v1/auth/login")"
[[ "$code" == 200 ]] || fail "the bootstrap administrator could not sign in: $code"
name="$(curl -s -b "$JAR" "http://127.0.0.1:${LOCAL_PORT}/api/v1/auth/me" | jq -r '.installation.name')"
[[ "$name" == Trial ]] || fail "signed in to the installation [$name], want [Trial]"
ok "the bootstrap administrator signs in to the installation the Job made"
# Kroki runs as the chart locks every pod down: its own user, no capability, a root file system it cannot write.
svg="$(curl -s -b "$JAR" -H 'Content-Type: application/json' -d '{"language":"plantuml","source":"@startuml\nA -> B\n@enduml"}' \
  "http://127.0.0.1:${LOCAL_PORT}/api/v1/diagrams" | jq -r '.svg // empty')"
[[ "$svg" == *"<svg"* ]] || fail "the api did not draw a diagram through Kroki in the cluster"
ok "the api draws a PlantUML diagram through Kroki, locked down as the chart runs it"
for partner in armature stator; do
  base="$(curl -s -b "$JAR" "http://127.0.0.1:${LOCAL_PORT}/api/v1/${partner}/connection" | jq -r '.connection.baseUrl')"
  [[ "$base" == "http://${partner}.chart-test.example" ]] || fail "the $partner connection is [$base], want the address the values name"
done
ok "the bootstrap Job connected Armature and Stator, which do not answer yet"

say "The work the worker and the bucket do"
api() { curl -s -b "$JAR" "$@"; }
KEY=CHART
created="$(api -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d "{\"key\":\"$KEY\",\"name\":\"Chart\"}" \
  "http://127.0.0.1:${LOCAL_PORT}/api/v1/projects")"
[[ "$created" == 201 ]] || fail "the administrator could not make a project: $created"
req="$(api -H 'Content-Type: application/json' -d '{"title":"The chart installs","statementMarkdown":"The chart installs in one go."}' \
  "http://127.0.0.1:${LOCAL_PORT}/api/v1/projects/${KEY}/requirements" | jq -r '.requirement.key // empty')"
[[ "$req" == "${KEY}-R1" ]] || fail "the administrator could not write a requirement: [$req]"

FILE="$(mktemp)"
printf 'Kept in the bundled bucket %s\n' "$(date +%s%N)" >"$FILE"
attachment="$(api -F "file=@${FILE};filename=evidence.txt;type=text/plain" \
  "http://127.0.0.1:${LOCAL_PORT}/api/v1/requirements/${req}/attachments" | jq -r '.attachment.id // empty')"
[[ -n "$attachment" ]] || fail "the attachment was not taken"
api -o "$FILE.back" "http://127.0.0.1:${LOCAL_PORT}/api/v1/attachments/${attachment}"
cmp -s "$FILE" "$FILE.back" || fail "the attachment read back is not the file uploaded: $(head -c 200 "$FILE.back")"
ok "an attachment goes into the bundled SeaweedFS and comes back the same"

printf 'Title,Statement\nImported by the worker,The worker imports this row.\nAnd this one,The worker imports this row too.\n' >"$FILE.csv"
import="$(api -F "file=@${FILE}.csv;filename=chart.csv;type=text/csv" "http://127.0.0.1:${LOCAL_PORT}/api/v1/projects/${KEY}/import" | jq -r '.import.id // empty')"
[[ -n "$import" ]] || fail "the CSV file was not taken for an import"
started="$(api -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${LOCAL_PORT}/api/v1/imports/${import}/start")"
[[ "$started" == 202 ]] || fail "the import did not start: $started"
state=""
for _ in $(seq 1 "$WORKER_WAIT"); do
  state="$(api "http://127.0.0.1:${LOCAL_PORT}/api/v1/imports/${import}" | jq -r '.import.state + " " + (.import.imported | length | tostring)')"
  [[ "$state" == "done "* || "$state" == "failed "* ]] && break
  sleep 1
done
[[ "$state" == "done 2" ]] || fail "the import reads [$state] after ${WORKER_WAIT}s, want done with two rows: only the worker finishes one"
ok "the worker imported both rows of a CSV file"

psql_count() { kubectl exec -n "$NS" "statefulset/${FULL}-postgresql" -c postgresql -- psql -U postgres -d exciter -tAc "$1"; }
left=""
for _ in $(seq 1 "$WORKER_WAIT"); do
  left="$(psql_count 'SELECT count(*) FROM outbox_event WHERE processed_at IS NULL')"
  [[ "$left" == 0 ]] && break
  sleep 1
done
[[ "$left" == 0 ]] || fail "$left outbox events are still unprocessed after ${WORKER_WAIT}s: $(psql_count "SELECT string_agg(topic || ' ' || attempts || ' ' || coalesce(last_error, ''), '; ') FROM outbox_event WHERE processed_at IS NULL")"
[[ "$(psql_count 'SELECT count(*) FROM outbox_event WHERE processed_at IS NOT NULL')" -gt 0 ]] || fail "the outbox has no event done, so the worker did nothing"
ok "the worker has handled every event in the outbox"

pdf="$(api --max-time "$PDF_WAIT" -o "$FILE.pdf" -w '%{http_code}' "http://127.0.0.1:${LOCAL_PORT}/api/v1/projects/${KEY}/document.pdf")"
[[ "$pdf" == 200 && "$(head -c 4 "$FILE.pdf")" == "%PDF" ]] || fail "the project's PDF export answered $pdf: $(head -c 300 "$FILE.pdf")"
ok "the render service prints the project as a PDF"
rm -f "$FILE" "$FILE.back" "$FILE.csv" "$FILE.pdf"

say "Upgrade"
before="$(kubectl get jobs -n "$NS" -o json | jq -r '[.items[] | select(.metadata.name | test("-migrate")) | .metadata.uid][0]')"
( while :; do
    curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 "http://127.0.0.1:${LOCAL_PORT}/healthz" >>"$JAR.health" 2>/dev/null || echo 000 >>"$JAR.health"
    sleep 0.5
  done ) &
WATCH_PID=$!
helm upgrade "$RELEASE" "$ROOT/deploy/charts/exciter" -n "$NS" -f "$ROOT/scripts/chart-install-values.yaml" --set api.replicas=2 --wait --timeout 15m
kill "$WATCH_PID" 2>/dev/null || true
after="$(kubectl get jobs -n "$NS" -o json | jq -r '[.items[] | select(.metadata.name | test("-migrate")) | .metadata.uid][0]')"
[[ -n "$after" && "$before" != "$after" ]] || fail "the migrate Job did not run again on upgrade"
ok "the migrate Job ran again"
[[ "$(kubectl get "deploy/${FULL}-api" -n "$NS" -o jsonpath='{.status.readyReplicas}')" == 2 ]] || fail "the api did not roll to two ready pods"
ok "the api is at two ready pods"
failed="$(grep -vc '^200$' "$JAR.health" || true)"
[[ "$failed" == 0 ]] || fail "$failed health checks failed during the upgrade"
ok "no health check failed while the api rolled ($(wc -l <"$JAR.health") made)"

say "The chart installs, upgrades, connects its partners, signs its administrator in and does its work"
