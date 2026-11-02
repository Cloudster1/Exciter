commit: 46af61fd9158c40da70617e8510d82defa4514df
source: docs/architecture.md

How Rotor is built, drawn from its architecture document, its decisions, its integration and trigger documents, the
compose file and Helm chart, and the code at this commit. Rotor has not released yet. Each view below is one part:
the processes and stores, how they are deployed, how people sign in, how assistants and the suite's service account
call the api, how Rotor reaches the tools, how a run goes from queued to its end in a sandbox, how a run gets its
tokens, and the triggers that start runs.

## Components and data stores

The browser loads the React web client from nginx, which proxies `/api` and the OAuth `.well-known` documents to the
Go api, so the browser sees one origin and follows a run's events as server-sent events. The api and the worker are
the trusted control plane: they keep workflows, runs, checkpoints and the audit log in Postgres under row level
security, use Valkey for the tools' cached `tools/list` and the signals that wake a run's event stream or carry a
cancel, and keep every long-lived secret in OpenBao: refresh tokens, provider keys, webhook secrets and the transit
key that wraps each run's data key. The worker starts one sandbox per run; the runner in it, Python and LangGraph,
dials the api over gRPC and calls the tools' MCP endpoints and the model provider as the run's person.

```plantuml
@startuml
title Components and data stores
skinparam componentStyle rectangle

actor "Person" as person
component "web\n(nginx, React SPA)" as web
component "Keycloak\n(realm dynamo)" as kc
component "api\n(Go, chi;\nRunnerControlService :9443)" as api
component "worker\n(Go)" as worker
rectangle "Control plane stores" as stores {
  database "Postgres\n(rotor_app, rotor_admin)" as pg
  database "Valkey\n(tools/list cache,\nrun event signals)" as valkey
  database "OpenBao\n(refresh tokens, keys,\nwrapping, transit)" as bao
}
rectangle "Sandbox, one per run" as sandbox {
  component "runner\n(Python, LangGraph)" as runner
}
cloud "Exciter, Armature\n(MCP, OAuth 2.1)" as tools
cloud "LLM provider" as llm

person --> web
web --> api : /api, SSE
api -left-> kc : sign-in, service account
api --> pg
api --> valkey
api --> bao
worker --> pg : leases runs
worker --> bao : forgets run tokens
worker --> sandbox : start, destroy
worker --> tools : revokes run tokens
runner -up-> api : gRPC, dialled out
runner -up-> bao : unwrap once
runner --> tools : MCP as the person
runner --> llm : model calls
@enduml
```

## Deployment with the Helm chart

The chart `deploy/charts/rotor` installs the api, the worker and the web, at their defaults two, one and two
replicas, and runs the secrets, OpenBao, roles and migrate hook Jobs before every upgrade. The Ingress sends
everything to the web, whose nginx proxies `/api` to the api on the same origin. The chart makes the sandbox
namespace, under the restricted Pod Security standard, with a token-less ServiceAccount and a Role that lets the
worker manage pods and Secrets there and nothing else. Sandboxes run under the gVisor RuntimeClass, dial the api's
Service on its runner port, and leave only by a NetworkPolicy that allows DNS, the runner port and OpenBao, with
Cilium allowing the tools and the provider by hostname. Postgres and Valkey are the installer's or bundled for a
trial; OpenBao and Keycloak are the installer's. In development `deploy/docker-compose.yml` runs the same images, and
the worker starts sandboxes on the host's Docker daemon on the internal network `sandbox`.

```plantuml
@startuml
title Deployment with the Helm chart
skinparam componentStyle rectangle

actor "Browser" as browser
node "Release namespace" {
  component "Ingress, one host" as ing
  component "web\n(nginx, 2 replicas)" as web
  component "Service api\n(http, runner, metrics)" as svc
  node "Deployment api\n(2 replicas)" as api
  node "Deployment worker\n(1 replica)" as worker
  rectangle "Hook Jobs" as jobs {
    component "secrets" as jsec
    component "openbao\n(optional)" as jbao
    component "roles" as jroles
    component "migrate" as jmig
  }
}
node "Sandbox namespace\n(restricted Pod Security)" {
  component "Role\n(pods, Secrets)" as role
  node "Pod per run\n(gVisor, no ServiceAccount token)" as pod
  component "NetworkPolicy,\nCiliumNetworkPolicy" as egress
}
database "Postgres\n(bundled for a trial,\nor your own)" as pg
database "Valkey\n(bundled for a trial,\nor your own)" as valkey
database "OpenBao\n(your own)" as bao
component "Keycloak\n(your own)" as kc
cloud "Tools and\nmodel provider" as tools

browser --> ing : https
ing --> web : /
web --> svc : /api, /.well-known/oauth-*
svc --> api
api --> pg
api --> valkey
api --> bao : Kubernetes auth
api --> kc
worker --> pg
worker --> role : manages pods and Secrets
role --> pod
pod --> svc : runner port
pod --> bao : unwrap
pod --> tools : allowed by hostname
egress .. pod
jsec -> jbao
jbao -> jroles
jroles -> jmig
jobs --> pg : roles, migrations
jbao --> bao : configure
@enduml
```

## Signing in through the identity provider

Sign-in is the authorization code flow with PKCE against the realm `dynamo`, client `rotor`, so a person is one
subject across the suite. `oidc_login` keeps the SHA-256 of the state with the nonce, the verifier and where to go
next, good for one callback. The callback matches the person in `user_identity` by issuer and subject and copies
their realm groups, where `rotor-administrators` makes an administrator; the session is a `user_session` row holding
the SHA-256 of an opaque token sent as the HttpOnly cookie `rotor_session`. Keycloak's back-channel logout reaches
`/api/v1/auth/oidc/backchannel-logout` and ends the matching sessions, and after a sign-out from everywhere Rotor
reads the person's sign-out time from Keycloak's admin API and ends their older sessions.

```plantuml
@startuml
title Signing in through the identity provider
autonumber

actor "Browser" as b
participant "api" as api
database "Postgres" as db
participant "Keycloak\n(realm dynamo)" as kc

b -> api : GET /api/v1/auth/oidc/start?next=...
api -> db : insert oidc_login\n(state hash, nonce, PKCE verifier)
api --> b : 302 to the provider\n(state, nonce, S256 challenge)
b -> kc : sign in, client rotor
kc --> b : 302 to /api/v1/auth/oidc/callback?code&state
b -> api : GET /api/v1/auth/oidc/callback
api -> db : take oidc_login by state hash, once
api -> kc : redeem the code with the verifier
kc --> api : id token
api -> api : verify the id token and nonce
api -> db : match user_identity (issuer, subject),\ncopy role and groups,\ninsert user_session (token SHA-256)
api --> b : 302 to next,\nSet-Cookie rotor_session\n(HttpOnly, SameSite=Lax)
== Signing out ==
kc -> api : POST /api/v1/auth/oidc/backchannel-logout\n(logout token)
api -> db : end the sessions with that sid
api -> kc : admin API: the person's sign-out time\n(service account, view-users)
api -> db : end their sessions older than it
@enduml
```

## Assistants and the suite's service account

Rotor is also an OAuth authorization server for its own MCP endpoint, `/api/v1/mcp` (`internal/oauth`, adapted from
Exciter's). An assistant registers by RFC 7591 or names a client ID metadata document, its person consents at
`/oauth/consent`, and it gets a `rotor_oat_` access token for MCP alone; Postgres keeps only digests of codes and
tokens. Each MCP tool is a row of the operations table, run in process through the router with the assistant's
token, so it does what its HTTP call does as the person; the transaction's `app.via` names the grant, the audit log
names the assistant, and an approval decided through one is refused. A bearer token that is not Rotor's own is let
in only on the four routes marked `serviceAccount`, where the suite's `dynamo-connect` adds tools and hands over
their webhook secrets with a realm access token that carries `rotor-administrators`.

```plantuml
@startuml
title Assistants and the suite's service account
autonumber

actor "Person" as p
participant "Assistant" as a
participant "dynamo-connect" as dc
participant "api" as api
database "Postgres" as db
participant "Keycloak\n(realm dynamo)" as kc

== An assistant connects ==
a -> api : POST /api/v1/mcp
api --> a : 401, resource_metadata
a -> api : GET /.well-known/oauth-protected-resource,\n/.well-known/oauth-authorization-server
a -> api : POST /api/v1/oauth/register (RFC 7591)
a -> api : GET /api/v1/oauth/authorize (PKCE S256)
p -> api : consent at /oauth/consent
api -> db : digest of the code
a -> api : POST /api/v1/oauth/token
api -> db : digests of rotor_oat_ and rotor_ort_
== Working over MCP ==
a -> api : POST /api/v1/mcp, tools/call start_run\nBearer rotor_oat_...
api -> api : the operation's route,\nin process, as the person
api -> db : transaction with app.via set to the grant;\naudit names the assistant
note over a, db : approvals are decided in Rotor's pages alone
== The suite's service account ==
dc -> kc : client credentials
kc --> dc : access token, groups rotor-administrators
dc -> api : POST /api/v1/tools, PUT /api/v1/tools/{toolID}/hook\nBearer, no cookie
api -> api : serviceAccount: realm keys, issuer,\naudience rotor, expiry, group
api -> db : act as dynamo-connect@service-account.invalid
@enduml
```

## Reaching the suite

Rotor is an OAuth 2.1 client of each tool and calls nothing of theirs but `/meta`, their OAuth endpoints and their
MCP endpoints, as the person. An administrator adds a tool by its address: Rotor reads its `/meta` and adds it only
with `mcp@1`, discovers the MCP endpoint's authorization server (RFC 9728, RFC 8414) and registers as its client (RFC
7591). A person connects on the tool's own consent page; the callback checks the state and the issuer, keeps the
refresh token in OpenBao at `rotor/connections/{user}/{tool}` and the granted scopes in Postgres, and drops the
access token. Exciter connects as it is; Armature once one of its administrators allows Rotor's client, and with
every run's token exchanged from the grant's. Stator cannot be added until it serves `mcp@1`. The other way, a tool
with `webhooks@1` sends signed deliveries to `/api/v1/hooks/{toolID}`, and Rotor lists `workflows@1`, `runs@1`,
`hooks@1` and `mcp@1` at its own `/meta` for the suite.

```plantuml
@startuml
title Reaching the suite

actor "Administrator" as adm
actor "Person" as p
participant "api" as api
database "OpenBao" as bao
database "Postgres" as db
participant "Exciter or Armature" as tool

== Adding a tool, once ==
adm -> api : POST /api/v1/tools {baseURL}
api -> tool : GET /api/v1/meta
tool --> api : capabilities: mcp@1 needed,\nwebhooks@1 for event triggers
api -> tool : 401 of /mcp, then RFC 9728\nand RFC 8414 metadata
api -> tool : register as an OAuth client (RFC 7591)
api -> db : the tool, its capabilities and client
== Connecting a person ==
p -> api : POST /api/v1/tools/{toolID}/connection
api --> p : authorizeURL (PKCE S256, resource is /mcp)
p -> tool : consent on the tool's own page
tool --> p : 302 to /api/v1/connections/callback?code&state&iss
p -> api : the callback
api -> tool : exchange the code
api -> bao : refresh token at\nrotor/connections/{user}/{tool}
api -> db : granted scopes; access token dropped
== Events back ==
tool -> api : POST /api/v1/hooks/{toolID}\nsigned, timestamped
note over api, tool : Stator cannot be added until it serves mcp@1
@enduml
```

## A run from queued to its end

A person starts a run with `POST /workflows/{id}/runs`: the inputs are checked against the workflow's schema, each
binding needs the person's connection with the scopes it asks for, and the run is inserted `queued`, sealed with a
data key of its own. Any number of workers lease runs with `FOR UPDATE SKIP LOCKED` and make each pass in order:
reconcile the stack's sandboxes, expire what waited or went silent, queue what triggers fired, start each queued run
in a sandbox with a single-use bootstrap token, and tear down after runs that ended. The runner dials
`RunnerControlService`, says `Hello`, gets `StartRun` with the pinned workflow and its model, compiles it into a
LangGraph graph and streams events and checkpoints back, each stored before it is acknowledged; the api publishes
`runevents:<run>` on Valkey so any api replica can stream the run to the browser. An approval node leaves the run
`waiting` and the worker drops its sandbox and tokens; a decision sends it back to `starting`, and a new sandbox
resumes from the last checkpoint.

```plantuml
@startuml
title A run from queued to its end
autonumber

actor "Person" as p
participant "api" as api
database "Postgres" as db
participant "Valkey" as v
participant "worker" as w
participant "sandbox\n(runner)" as r

p -> api : POST /api/v1/workflows/{id}/runs\nIdempotency-Key
api -> api : inputs against the schema,\na connection per binding
api -> db : insert run queued,\nsealed with its data key
w -> db : lease queued runs\nFOR UPDATE SKIP LOCKED
w -> r : start the sandbox,\nsingle-use bootstrap token
r -> api : Connect (gRPC :9443), Hello
api --> r : HelloAck, StartRun\n(pinned spec, bindings, model)
loop until the run ends or waits
  r -> api : Event, CheckpointPut
  api -> db : store, then acknowledge
  api -> v : publish on the run's channel
end
p -> api : GET /api/v1/runs/{id}/events (SSE)
alt an approval node
  r -> api : Done, interrupted
  api -> db : run waiting, approval row
  w -> r : destroy the sandbox, revoke tokens
  p -> api : decide the approval
  api -> db : run starting again
  w -> r : a new sandbox resumes\nfrom the last checkpoint
else the graph finishes
  r -> api : Done, succeeded or failed
  w -> r : teardown, revoke tokens
end
@enduml
```

## A run's tokens through OpenBao

A sandbox holds no refresh token and no OpenBao credential, and never sees a token in clear on the wire. When the
runner asks for a binding's token, the api mints one as the run's person from the connection's refresh token, one
refresh per connection at a time; for a tool that keeps one access token per grant, as Armature does, it exchanges
the grant's token for one narrowed to the binding's scopes, for 15 minutes (RFC 8693). It keeps the token at
`rotor/runs/{run}/tokens/{id}` and sends a `TokenGrant` holding an OpenBao wrapping token that opens once within two
minutes; the runner unwraps it at OpenBao itself, and the model provider's key comes the same way as a `SecretGrant`.
Tokens live in the runner's memory alone and are cut from its events; when the run ends or waits, the worker revokes
each one at its tool and forgets it.

```plantuml
@startuml
title A run's tokens through OpenBao
autonumber

participant "runner\n(sandbox)" as r
participant "api\n(runnerctl)" as api
database "OpenBao" as bao
participant "Tool\n(Exciter, Armature)" as tool
participant "worker" as w

r -> api : TokenRequest for a binding
api -> bao : read the refresh token\nrotor/connections/{user}/{tool}
alt one token per grant, as Armature
  api -> tool : token exchange (RFC 8693),\nthe binding's scopes, 15 minutes
else other tools
  api -> tool : refresh, one at a time per connection
end
tool --> api : access token for the run's person
api -> bao : keep it at rotor/runs/{run}/tokens/{id}
api -> bao : wrap it, opens once within two minutes
api --> r : TokenGrant (wrapping token)
r -> bao : unwrap once
r -> tool : MCP calls as the person
note over r, api : the provider key comes the same way, in a SecretGrant
w -> tool : revoke each token when the run ends or waits
w -> bao : forget the run's tokens
@enduml
```

## Triggers from schedules and tool events

A trigger is a record a person makes on a workflow they may see; it starts the workflow's current version as its
owner, on a schedule or when a tool delivers a matching event. A delivery to `/api/v1/hooks/{toolID}` must carry the
tool's timestamped HMAC-SHA256 signature, at most five minutes old, made with the secret kept in OpenBao; its event
id goes into `hook_event` so a repeat starts nothing, and each enabled event trigger whose topics, projects and
conditions match gets a `trigger_fire` row in the same transaction. The worker sweeps off, once a minute, the
triggers whose owner, connection or workflow is gone, and on every pass turns due schedules into firings under an advisory lock, checks every
firing again as the trigger's owner and calls `runs.Start` with the exact version it checked and a key per firing,
so a firing never starts two runs. A failed check turns the trigger off with a reason the owner reads.

```plantuml
@startuml
title Triggers from schedules and tool events
skinparam componentStyle rectangle

component "Exciter or Armature\nwebhook" as tool
component "api\nPOST /api/v1/hooks/{toolID}" as hooks
database "OpenBao\n(webhook secret)" as bao
database "hook_event\n(ids kept seven days)" as seen
database "trigger\n(schedule or event)" as trig
database "trigger_fire\n(queued firings)" as fire
package "worker pass" {
  component "Sweep\n(once a minute)" as sweep
  component "Schedule\n(advisory lock, cron\nin the trigger's zone)" as sched
  component "Fire\n(checked as the owner)" as firer
}
component "runs.Start\n(exact version,\na key per firing)" as start
database "run\n(queued)" as run

tool --> hooks : signed, timestamped
hooks --> bao : secret
hooks --> seen : once per event id
hooks --> fire : a firing per matching trigger
sched --> trig : due schedules
sched --> fire
firer --> fire : claim
firer --> start
start --> run
sweep --> trig : off when owner, connection\nor workflow is gone
firer --> trig : a failed check turns it off
@enduml
```
