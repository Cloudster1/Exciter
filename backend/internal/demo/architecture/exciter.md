commit: b4e13d03c9a3783475defb946576743738e29d43
source: docs/architecture.md

How Exciter is built, drawn from its architecture document, its compose file and Helm chart, and the code behind them. Each view below is one part: the processes and stores, how they are deployed, how people sign in, how Exciter reaches Armature and Stator, and the outbox, revisions and row level security underneath.

## Components and data stores

The browser loads the React web client from nginx, which proxies `/api` to the Go api, so the browser sees one origin. The api writes to the Postgres primary and reads from a streaming replica once it has caught up, keeps read-your-writes positions and per-token caches of Armature and Stator answers in Valkey, and keeps attachments and theme files in an S3 bucket. Two sidecars only the api reaches do the heavy drawing: `render`, a headless Chromium that prints the web client's print view as PDF, and `kroki`, which draws PlantUML in its secure mode. The worker drains the outbox, sends mail, removes files whose rows are gone and puts remote links on Armature issues and Stator pages.

```plantuml
@startuml
title Components and data stores
skinparam componentStyle rectangle

actor "Person" as person
component "web client\n(React SPA)" as spa
component "web\n(nginx)" as web
component "Keycloak\n(OIDC provider)" as kc
component "api\n(Go)" as api
rectangle "Sidecars, internal only" as sidecars {
  component "render\n(headless Chromium)" as render
  component "kroki\n(PlantUML, secure mode)" as kroki
}
rectangle "Data stores" as stores {
  database "Postgres\nprimary" as pg
  database "Postgres\nreplica" as replica
  database "Valkey" as valkey
  storage "S3 bucket\n(SeaweedFS in dev)" as s3
}
cloud "Armature\nand Stator" as partners
component "worker\n(Go)" as worker
component "SMTP\n(Mailpit in dev)" as mail

person --> spa
spa --> web : /api/v1
web --> api : proxies /api
api -left-> kc : sign-in
api --> render : /pdf, /html
render .up.> web : opens the print view
api --> kroki : PlantUML to SVG
sidecars -[hidden]down- stores
api ---> pg : writes
api ---> replica : reads
api ---> valkey
api ---> s3
pg -> replica : WAL
api --> partners : reads as the viewer
partners --> api : webhooks
worker -up-> pg : claims outbox_event
worker -up-> s3 : reaps files
worker -up-> partners : puts remote links
worker -> mail
@enduml
```

## Deployment with the Helm chart

Outside development Exciter runs from the chart in `deploy/charts/exciter`: Deployments for web, api, worker, kroki and, once `render.enabled` is set, render, with only the Ingress published. The Ingress sends `/api` and the OAuth metadata under `/.well-known/` to the api and everything else to web; hook Jobs make the secrets and database roles, run the migrations as the owner, and bootstrap the installation. Postgres comes from CloudNativePG or your own, Valkey and S3 from your own, each bundled for a trial. In development the same images run under Docker Compose, every port bound to `127.0.0.1`, with Keycloak, Mailpit and SeaweedFS beside them, and kroki and render publishing none.

```plantuml
@startuml
title Deployment with the Helm chart
skinparam componentStyle rectangle

actor "Browser" as browser
node "Release in a Kubernetes namespace" {
  component "Ingress, one host" as ing
  component "web :80\n(nginx, the SPA)" as web
  component "api\n(and its metrics port)" as api
  component "kroki :8000\n(on by default)" as kroki
  component "render :8090\n(off by default)" as render
  component "worker\n(no Service)" as worker
  rectangle "Hook Jobs" as jobs {
    component "secrets" as jsec
    component "roles" as jroles
    component "migrate" as jmig
    component "bootstrap" as jboot
  }
  rectangle "Data: bundled for a trial, or your own" as stores {
    database "Postgres\n(CloudNativePG\n-rw and -ro)" as pg
    database "Valkey" as valkey
    storage "S3 bucket\n(SeaweedFS subchart)" as s3
  }
}

browser --> ing : https
ing --> web : /
ing --> api : /api, /.well-known/oauth-*
web -> api : proxies /api
api --> kroki
api --> render
render .up.> web : print view
api ---> stores
worker --> stores
jsec -> jroles
jroles -> jmig
jmig -> jboot
jobs --> stores : migrate as the owner
pg -[hidden]right- valkey
valkey -[hidden]right- s3
@enduml
```

## Signing in through the identity provider

Single sign-on is the authorization code flow with PKCE against the installation's one OIDC provider, in development Keycloak's realm `dynamo`, shared with Armature and Stator so a person is one subject in all three. The state, nonce and verifier wait in `oidc_login` and are taken once; the person is matched by issuer and subject in `user_identity`. When token exchange is switched on, the api trades the Keycloak access token for the person's tokens at Armature and Stator before the browser lands, and a failed exchange never fails the sign-in. The session lives in the http-only cookie `exciter_session`, Secure outside local development, and the database keeps only its SHA-256.

```plantuml
@startuml
title Signing in through the identity provider
autonumber

actor "Browser" as b
participant "api" as api
database "Postgres" as db
participant "Keycloak\n(realm dynamo)" as kc
participant "Armature, Stator" as partners

b -> api : GET /api/v1/auth/oidc/start?next=...
api -> db : insert oidc_login\n(state, nonce, PKCE verifier)
api --> b : 302 to the provider\n(state, nonce, S256 challenge)
b -> kc : sign in, client exciter
kc --> b : 302 to /api/v1/auth/oidc/callback?code&state
b -> api : GET /api/v1/auth/oidc/callback
api -> db : delete oidc_login by state\n(single use, returns verifier)
api -> kc : redeem code with verifier\n(EXCITER_OIDC_BACKCHANNEL)
kc --> api : id token, access token
api -> api : verify id token and nonce\nagainst the issuer's keys
api -> db : match user_identity (issuer, subject),\nsync groups, open user_session\n(stores the secret's SHA-256)
opt token exchange switched on
  api -> partners : POST /api/v1/oauth/token\n(RFC 8693, the Keycloak access token)
  partners --> api : the person's token, sealed\nin armature_token and stator_token
end
api --> b : 302 into the app,\nSet-Cookie exciter_session\n(HttpOnly, SameSite=Lax)
@enduml
```

## Passwords, the session cookie and tokens

A local account signs in with its password, checked against an argon2id hash, and gets the same `exciter_session` cookie as single sign-on. There is no CSRF token: a write that rides on the cookie must come from the app's own origin and carry JSON or multipart, which a cross-site form cannot send. A script or pipeline sends a personal access token, `exciter_pat_` and 43 characters, as a bearer; it acts as its owner, a read-only one is refused every write before any handler, and one limited to projects carries them into every transaction.

```plantuml
@startuml
title Passwords, the session cookie and tokens
autonumber

actor "Browser" as b
actor "Script or CI" as ci
participant "api" as api
database "Postgres" as db

== Password sign-in ==
b -> api : POST /api/v1/auth/login\n{email, password}
api -> db : as exciter_admin: app_user,\nargon2id hash, membership
api -> db : open user_session\n(stores the secret's SHA-256)
api --> b : 200, who is signed in;\nSet-Cookie exciter_session

== A write from the browser ==
b -> api : PATCH /api/v1/... with the cookie
api -> api : same-site check: Origin is the app's,\nbody is JSON or multipart
api -> db : find user_session by SHA-256,\nstill a member?
api -> db : transaction as exciter_app,\napp.user_id set

== A personal access token ==
ci -> api : Authorization: Bearer exciter_pat_...
api -> db : find api_token by SHA-256
api -> api : read-only token: every write refused
api -> db : transaction as exciter_app,\napp.user_id, app.token_projects, app.via
note over ci, api
  An OAuth access token, exciter_oat_..., is let in
  at /api/v1/mcp only, unless it is an evidence
  grant or a partner app's.
end note
@enduml
```

## Reaching Armature and Stator

Exciter holds one connection to each partner, its address, the webhook secret the partner showed and, for token exchange, Exciter's client id and secret there, all sealed with `EXCITER_SECRET_KEY`; each person's token is sealed for that person alone. Every read of an issue or a page is made with the viewer's own token, through the outbound guard, so Exciter shows only what the partner would show them. Links go out through the outbox: a trigger emits `armature.links` or `stator.links`, and the worker puts the remote links as the person who made the change. The partners' signed webhooks come back to `/api/v1/armature/webhook` and `/api/v1/stator/webhook` and keep issue statuses and page versions current.

```plantuml
@startuml
title Reaching Armature and Stator

actor "Person" as p
participant "api" as api
database "Postgres" as db
participant "worker" as w
participant "Armature" as arm
participant "Stator" as st

== Connecting, once per installation ==
p -> api : save the connection:\nbaseUrl, webhookSecret,\nclientId, clientSecret
api -> arm : GET /api/v1/meta
api -> db : armature_connection,\nsecrets sealed
== Each person's token ==
p -> api : paste a token, or\nsign in to exchange one
api -> db : armature_token,\nsealed per person
== Reading as the viewer ==
p -> api : open a requirement
api -> arm : GET /issues/{key}\nwith the viewer's token
api -> st : GET /pages/{id}\nwith the viewer's token
== Linking ==
p -> api : link an issue
api -> db : armature_issue_link;\na trigger emits\narmature.links
w -> db : claim armature.links
w -> arm : POST /issues/{key}/\nremote-links, as\nthe person who linked
w -> db : armature_remote_link
note over w, st : stator.links puts remote\nlinks on pages the same way
== Hearing back ==
arm -> api : POST /api/v1/armature/webhook\nissue.transitioned, signed
api -> db : check the signature,\nstore the status category
st -> api : POST /api/v1/stator/webhook\npage.published, signed
api -> db : store the page version;\nolder links turn suspect
@enduml
```

## The event outbox and the worker

A change and the events it causes commit together: `events.Emit` writes `outbox_event` in the caller's transaction, and the app role may insert only events whose actor is the person it acts for. Any number of workers claim events with `FOR UPDATE SKIP LOCKED`, queue outbound webhook deliveries first, then route each topic through `events.Mux` to its handler, which must be safe to run twice. A failure is retried after a growing delay, and the tenth leaves an `event.dead_lettered` entry in the audit log so one bad event never blocks the rest.

```plantuml
@startuml
title The event outbox and the worker

|api request|
start
:change rows as exciter_app;
:events.Emit writes outbox_event
in the same transaction;
note right
  actorId must be the
  acting person; triggers
  write armature.links
  and stator.links
end note
:commit;
|worker|
repeat
  :claim up to 20 events
  FOR UPDATE SKIP LOCKED,
  leased for 5 minutes;
  :queue a webhook_delivery for
  each endpoint on the topic;
  switch (events.Mux, by topic)
  case (import.requested\nreqif.requested)
    :run the import as
    the person who asked;
  case (armature.links\nstator.links)
    :put or take off
    remote links;
  case (requirement.*,\nreview.* and more)
    :notifications
    and mail;
  endswitch
  if (handled?) then (yes)
    :mark done;
  elseif (tenth attempt?) then (yes)
    :mark done, audit log
    event.dead_lettered;
  else (no)
    :retry after a
    growing delay;
  endif
repeat while (next pass, a second later when idle)
@enduml
```

## Revisions, baselines and suspect links

A save that changes a requirement's normative content writes the next numbered, immutable revision; status, owner, labels and position are logged but never revisioned. Reviews, baselines and links all point at revision numbers, so a baseline is the list of revisions as one transaction found them. A trace link remembers the revision of each end it was confirmed against and turns suspect, computed on read in `trace_link_state`, when an end its type watches has a newer one; clearing confirms it again and is recorded for good.

```plantuml
@startuml
title Revisions, baselines and suspect links
skinparam componentStyle rectangle

component "requirement\n(current revision number,\nstatus, owner, place in the tree)" as req
component "requirement_revision\n(numbered 1, 2, 3, never changed:\ntitle, statement, rationale,\nattributes, content hash)" as rev
component "trace_link\n(source_reviewed_rev,\ntarget_reviewed_rev)" as link
component "trace_link_state\n(view: suspect when a watched\nend has a newer revision)" as state
component "trace_link_clearance\n(who confirmed, against what)" as clear
component "baseline\n(a project or a section)" as base
component "baseline_item\n(requirement at its revision)" as item
component "requirement_review\n(of one revision)" as review
component "armature_issue_link\nstator_page_link\n(reviewed_rev; a page link\nalso the page version seen)" as ext

req --> rev : a save of normative\ncontent writes the next
link --> req : two ends
state ..> link : computed on read
state ..> rev
clear --> link : clearing confirms\nboth ends
base --> item : taken in one\ntransaction
item --> rev
review --> rev
ext --> req
@enduml
```

## Row level security and the database roles

Every table forces row level security, and the api's requests run as `exciter_app` with the acting person, the token's projects and the token itself set in the transaction. A permissive policy per table lets the role in, and restrictive policies built on SQL functions such as `perm_project_holds` are the walls, so a guard holds even for a query that forgot it, and a test proves a guard by trying the same thing in SQL. `exciter_admin` alone reaches accounts and sessions, and the owner that runs the migrations is `BYPASSRLS`.

```plantuml
@startuml
title Row level security and the database roles

participant "handler" as h
participant "db.Cluster" as c
database "Postgres,\nas exciter_app" as pg
participant "policies on\nrequirement" as pol
participant "perm_project_holds" as perm

h -> c : Read or Write,\nacting person\non the context
c -> pg : BEGIN, on a caught-up\nreplica for a read
c -> pg : set_config app.user_id,\napp.token_projects,\napp.via
h -> pg : SELECT ... FROM\nrequirement
pg -> pol : each row
pol -> pol : requirement_installation\n(permissive)
pol -> perm : requirement_visible\n(restrictive):\nproject, actor, 'view'
perm -> perm : project_grant to the\nperson, a group or\neveryone; owners and\nadmins hold all;\ntoken projects narrow
perm --> pol : true or false
pol --> pg : keep or drop
pg --> h : only rows the actor\nmay view
c -> pg : COMMIT
note over h, perm
  exciter_admin alone reads accounts, sessions and sign-ins
  in flight. The owner runs the migrations and is BYPASSRLS,
  so the SECURITY DEFINER permission functions read what
  they need through FORCE ROW LEVEL SECURITY.
end note
@enduml
```
