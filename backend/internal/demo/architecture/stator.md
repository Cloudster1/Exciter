commit: 6094b76162be38a0ec13e12c00cd6a37b22219c9
source: docs/architecture.md

How Stator, the suite's wiki, is built, drawn from its architecture document and checked against its compose
file, Helm chart and backend code at the commit above.

## Components and data stores

Stator is a React single page app served by nginx, one Go `api` and one Go `worker`, both built from the same
backend image. The api writes to the Postgres primary and reads from a replica whenever that replica has replayed
the caller's last write; Valkey holds caches, read-your-writes positions and the bus between api processes, and
every file lives in S3-compatible storage. Two sidecars are reached by the api alone: `converter` turns office
documents into PDF previews, and `render` prints a page's print view to PDF by opening it in the web container.

```plantuml
@startuml
title Components and data stores
skinparam componentStyle rectangle

actor "Browser" as browser
component "OIDC provider\n(Keycloak)" as idp
component "web\nnginx, React SPA" as web
component "render\nheadless Chromium" as render
component "api (Go)" as api
component "converter\noffice to PDF" as converter
component "worker (Go)" as worker
component "Armature, Exciter\n(through netguard)" as partners
component "SMTP relay" as smtp
database "PostgreSQL\nprimary" as primary
database "PostgreSQL\nreplica" as replica
database "Valkey" as valkey
database "S3 bucket\nstator-files" as s3

browser --> web : HTTPS
browser -right-> idp : sign in
web --> api : /api/, /readyz,\ncollab WebSocket
api -up-> idp : code exchange
api -left-> render : print a page
render -up-> web : /print/p/{id}
api -right-> converter : office file
api --> primary : writes,\noutbox_event
api --> replica : reads,\nwhen fit
primary -right-> replica : streaming\nreplication
api --> valkey : cache, read-your-writes,\ncollab bus
api --> s3 : attachments,\ntheme assets
api --> partners : as the viewer
worker -up-> primary : leases\noutbox_event
worker --> s3 : exports,\nimports
worker --> smtp : mail
worker --> partners : link sync,\nwebhooks
@enduml
```

## Deployment with the Helm chart

`deploy/charts/stator` runs the api, the worker and the web behind one Ingress host: `/api` goes straight to the
api Service and everything else to the web Service, so the session cookie stays first-party. Hook Jobs make the
passwords, the two database roles and the schema before the workloads start; the database is a CloudNativePG
`Cluster` (`-rw` for writes, `-ro` across replicas) or, for a trial, the chart's own Postgres and Valkey pods. In
the compose stack every service publishes only on `127.0.0.1`, and `converter` and `render` publish nothing.

```plantuml
@startuml
title Deployment with the Helm chart
skinparam componentStyle rectangle

actor "Browser" as browser

node "Kubernetes release" {
  rectangle "Ingress\none host" as ing
  rectangle "Service web :80" as sweb
  rectangle "Service api\nhttp, metrics" as sapi
  rectangle "Deployment web\nnginx, SPA" as dweb
  rectangle "Deployment api" as dapi
  rectangle "Deployment worker" as dworker
  rectangle "Deployment render\nService :8090\n(render.enabled)" as drender
  rectangle "Hook Jobs\nsecrets, roles,\nmigrate, bootstrap" as jobs
  rectangle "CronJobs\nbackup, restore\n(suspended)" as backup
  rectangle "ConfigMap env\nSecrets by reference" as cfg
  rectangle "ServiceMonitor" as sm
  database "CNPG Cluster\nservices -rw and -ro\nor bundled Postgres" as pg
  database "Valkey\nbundled or your own" as valkey
  database "S3\nSeaweedFS or your own" as s3
}

browser --> ing : HTTPS
ing --> sapi : /api
ing --> sweb : /
sweb --> dweb
sapi --> dapi
dweb --> sapi : proxies /api,\nWebSocket
dapi --> drender
drender --> sweb : print views
dapi --> pg
dworker --> pg
dapi --> valkey
dapi --> s3
dworker --> s3
jobs --> pg : roles, migrations
backup --> pg : owner, -rw
backup --> s3
sm --> sapi : scrapes metrics
cfg ..> dapi
cfg ..> dworker
@enduml
```

## Signing in through the identity provider

An instance serves one organization, and its OIDC provider is configured in the database, not in the environment.
Signing in is the authorization code flow with PKCE: the api keeps the state, nonce and code verifier in
`oidc_login` for fifteen minutes, verifies the ID token and its nonce, makes or finds the person by issuer and
subject, syncs their groups and opens a session. The browser only ever holds the `stator_session` cookie, which
the database keeps as a SHA-256 digest.

```plantuml
@startuml
title Signing in through the identity provider
actor "Browser" as b
participant "api" as api
database "Postgres\n(stator_admin)" as db
participant "OIDC provider" as idp

b -> api : GET /api/v1/auth/oidc/start?next=/s/DOC
api -> db : INSERT oidc_login\n(state, nonce, code_verifier, redirect)
api --> b : 302 to the authorization endpoint\nstate, nonce, S256 code_challenge
b -> idp : sign in
idp --> b : 302 /api/v1/auth/oidc/callback?code&state
b -> api : GET /api/v1/auth/oidc/callback
api -> db : DELETE oidc_login WHERE state\nRETURNING nonce, code_verifier
api -> idp : token request: code, code_verifier
idp --> api : id_token
api -> api : verify signature, audience, nonce
api -> db : app_user and user_identity (issuer, subject),\ngroup sync, INSERT user_session (SHA-256)
api --> b : 302 to next\nSet-Cookie stator_session\nHttpOnly, SameSite=Lax, Secure
note over b, api
  Not a member, no verified email or a refusal
  lands on /login?sso=not_a_member (or another reason) instead.
end note
@enduml
```

## Passwords, sessions and access tokens

A local account signs in with `POST /api/v1/auth/login`, checked against an argon2 hash, and gets the same session
cookie. Every request then presents one credential: `Authorization: Bearer` wins over the cookie, a secret starting
`stator_pat_` is looked up in `api_token` and anything else in `user_session`, both by digest. Writes that ride on
the cookie are held to the site's origins and to JSON or multipart bodies instead of a CSRF token, and a token made
with the `read` scope is refused every write.

```plantuml
@startuml
title Passwords, sessions and access tokens
actor "Browser" as b
participant "API client\nor MCP" as c
participant "api" as api
database "Postgres" as db

== Password sign-in ==
b -> api : POST /api/v1/auth/login {email, password}
api -> db : read password_hash, verify argon2\n(a decoy hash for unknown emails)
api -> db : INSERT user_session (SHA-256 of the secret)
api --> b : 200, Set-Cookie stator_session

== A request with the cookie ==
b -> api : PUT /api/v1/pages/{id}/draft\nCookie: stator_session
api -> api : sameSite: Origin is the app's,\nbody is JSON or multipart
api -> db : user_session by digest, membership, role
api -> db : transaction as stator_app,\napp.user_id set

== A request with a personal access token ==
c -> api : GET /api/v1/search\nAuthorization: Bearer stator_pat_...
api -> db : api_token by digest:\nscopes, spaces, expiry
api -> api : read scope: refuse writes\n(403 read_only_token)
api -> db : transaction as stator_app,\napp.user_id, app.token_spaces
@enduml
```

## Reaching Armature and Exciter

Stator calls each partner as the viewing person, with that person's own token sealed under `STATOR_SECRET_KEY`
(an Armature personal access token or OAuth grant, an `exciter_pat_` token), so a page never shows an issue or a
requirement its reader could not open; answers are kept per token in Valkey and a signed webhook from the partner
clears them. On publish the worker puts a remote link on every Armature issue the page names. Exciter in turn
reads pages, puts its requirements on them as remote links with a `stator_pat_` token, and hears through Stator's
signed webhooks, whose `stator_whs_` secret an administrator pastes into Exciter.

```plantuml
@startuml
title Reaching Armature and Exciter
participant "Armature" as arm
participant "Stator api" as api
participant "Stator worker" as w
participant "Exciter" as exc

== A reader opens a page ==
api -> arm : issues, NQL lists, /meta\nas the reader (token or OAuth grant)
api -> exc : GET /requirements/{reqKey}, lists, /meta\nas the reader (exciter_pat_)
note over api : answers kept per token in Valkey for 60 s

== An author publishes ==
api --> w : outbox_event: armature.links, page.published
w -> arm : POST /issues/{key}/remote-links\nas the publisher, source Stator
w -> exc : POST page.published\nX-Stator-Signature-256 (stator_whs_)

== Exciter traces a requirement to the page ==
exc -> api : GET the page, /search/quick\n(stator_pat_)
exc -> api : POST /api/v1/pages/{id}/remote-links\n{url, title, source: Exciter}
w -> exc : page.remote_link.removed\nwhen a person takes one off

== Something changes in a partner ==
arm -> api : POST /api/v1/armature/webhook\nX-Armature-Signature-256
exc -> api : POST /api/v1/exciter/webhook\nX-Exciter-Signature-256 (exciter_whs_)
api -> api : clear the cached answers
@enduml
```

## Editing together and publishing

Everybody with a page's editor open edits one shared draft, a Yjs document passed over a WebSocket in
y-protocols' framing; the api stores each update and relays it without reading it, and a second api process hears
of it through Valkey or, without Valkey, Postgres's LISTEN and NOTIFY. Publishing is still each person's own: the
shared draft is saved into their `page_draft`, and the publish writes a `page_version`, copies it onto `page` and
queues its events in one transaction, so history, diff and restore read `page_version`.

```plantuml
@startuml
title Editing together and publishing
actor "Editor A" as a
participant "api process 1" as api1
database "Postgres" as db
participant "Valkey or\nLISTEN/NOTIFY" as bus
participant "api process 2" as api2
actor "Editor B" as b

a -> api1 : GET /pages/{id}/collab\nWebSocket, session,\nsame origin
api1 -> db : may edit?\njoin the room\n(page_collab)
api1 --> a : every stored update
a -> api1 : Yjs update
api1 -> db : INSERT\npage_collab_update\n(perm_page_editable)
api1 -> bus : the update, or\na pointer to its row
bus -> api2 : update
api2 --> b : relay
note over api1, api2 : every 30 s each connection checks\nthe session and the edit right again

a -> api1 : PUT /pages/{id}/draft\nshared draft as A's own
a -> api1 : POST /pages/{id}/publish
api1 -> db : one transaction:\nINSERT page_version,\nUPDATE page,\nINSERT outbox_event
api1 --> a : the new version
@enduml
```

## The outbox and the worker

Anything that must happen after a change, from mail to Armature's links and outbound webhooks, is an
`outbox_event` row committed with the change. Any number of workers lease batches of events with
`FOR UPDATE SKIP LOCKED`, so delivery is at least once and a worker that dies only delays its lease. Webhook
deliveries are queued per endpoint and sent by a loop of their own, each payload read as the endpoint's owner at
send time and retried six times; the worker also runs the watches for scheduled publishes, lapsed verifications,
due tasks, example spaces, imports and exports.

```plantuml
@startuml
title The outbox and the worker
skinparam componentStyle rectangle
left to right direction

component "api\nrequest transaction" as api
database "outbox_event" as outbox
component "events.Worker\nlease, SKIP LOCKED" as ew
component "armature.LinkSync" as sync
component "webhook.Service\nqueue per endpoint" as queue
component "notify.FanOut\nin-app and mail" as fan
database "webhook_delivery" as wd
component "webhook.Sender\nsigned, 6 attempts" as sender
component "watches\nscheduled publishes,\nverifications, due tasks" as watches
component "Armature" as arm
component "webhook endpoints\n(Exciter and others)" as ep
component "SMTP relay" as smtp

api --> outbox : INSERT with the change
watches --> outbox : events of their own
ew --> outbox : claim, settle
ew --> sync : armature.links
ew --> queue : every other topic
ew --> fan : every other topic
sync --> arm : remote links,\nas the person
queue --> wd
sender --> wd : due attempts
sender --> ep : POST, X-Stator-Signature-256
fan --> smtp
@enduml
```

## Row level security and search

The database itself decides who reaches which rows. The api and the worker connect as `stator_app`, which cannot
bypass row level security, and each transaction names its person in `app.user_id` (and, when they apply, a token's
spaces, an anonymous reader or a public link); every table has a permissive `_app` policy narrowed by restrictive
policies built on the same `perm_*` functions the services call. Search uses a stored `tsvector` with a GIN index:
hit functions match through the index first and apply the viewers' rules to the matches only.

```plantuml
@startuml
title Row level security and search
skinparam componentStyle rectangle

component "api handler\nGET /api/v1/search?q=" as h
component "db.WithUser\napp.user_id, app.token_spaces,\napp.anonymous, app.page_link" as ctx

package "PostgreSQL" {
  component "stator_app\nno bypass" as app
  component "stator_admin\nsign-in, webhook secrets,\nworker claims, migrations" as admin
  component "page_search_rows\nGIN on the stored tsvector,\nwebsearch_to_tsquery" as hits
  component "policies\n_app permissive,\naccess policies restrictive" as pol
  component "perm_* functions\nperm_page_viewable,\nperm_space_holds" as perm
  database "page, space, attachment,\ncomment and the rest" as tables
}

h --> ctx
ctx --> app : one transaction
app --> hits : search
app --> tables : every other read and write
hits --> tables : match through the index
hits --> perm : rights for the matches only
pol --> perm
tables --> pol : FORCE ROW LEVEL SECURITY
admin --> tables : _admin_bypass policies
@enduml
```
