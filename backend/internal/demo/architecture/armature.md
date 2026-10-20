commit: ef402b2acc81a919f2b95e43195d0c98c124ae91
source: docs/decisions.md

How Armature is built, drawn from its decisions document, its README, the compose file, the Helm chart and the code at
this commit. Armature has no architecture document of its own, so each view names what the code calls things.

## Components and data stores

The browser talks to one origin: the web client serves the pages and passes `/api` to the api, a Go server that
writes to the Postgres primary and reads from replicas. Every change writes its event to the `outbox_event` table in
the same transaction, and the worker relays those rows onto a Valkey stream and does everything that should not keep
a request waiting: webhooks, notifications, mail, automation and talking back to git hosts. Attachments live in an S3
bucket, but their bytes always pass through the api, so the database row decides who may read them.

```plantuml
@startuml
title Components and data stores
skinparam componentStyle rectangle

actor "Browser" as browser
component "web\n(React SPA, nginx or Vite)" as web
component "render\n(Chromium, PDF export)" as render
component "api\n(Go, chi)" as api
component "worker\n(Go)" as worker

database "Postgres primary\n(outbox_event included)" as primary
database "Postgres replicas" as replica
database "Valkey\n(stream armature:events,\nfreshness positions)" as valkey
database "S3 bucket\n(attachments)" as s3

component "Keycloak\n(OIDC provider)" as idp
component "Git host\n(Gitea, GitHub, GitLab)" as git
component "Mail relay\n(SMTP out, POP3 in)" as mail
component "Webhook receivers\n(Exciter, Stator, others)" as receivers

browser --> web : pages, /api
web --> api : /api proxied
api -left-> render : print a page
render -up-> web : loads the page
api -right-> idp : OIDC code exchange
git -up-> api : signed deliveries
api --> primary : writes and events
api --> replica : reads
primary -right-> replica : streaming replication
api --> valkey : freshness positions
api --> s3 : attachment bytes
worker -up-> primary : relay outbox rows
worker -up-> valkey : stream and groups
worker -up-> s3 : reap orphans
worker --> mail : notifications, desk replies
worker --> git : pull request comments
worker --> receivers : signed webhooks
@enduml
```

## Deployment with the Helm chart

The chart `deploy/charts/armature` installs the api, the worker, the web client and, when `render.enabled` is set,
the PDF renderer. One Ingress host carries everything: `/api` and the two OAuth `.well-known` documents go to the api
Service, the rest to the web Service, so the session cookie stays first party. The worker is pinned to one replica,
since its stream consumers have no leader election, and metrics stay on a private port that only a ServiceMonitor
scrapes. The compose stack runs the same images beside Keycloak, Gitea, Mailpit and SeaweedFS for development.

```plantuml
@startuml
title Deployment with the Helm chart
skinparam componentStyle rectangle

actor "Browser or\npartner tool" as client

node "Kubernetes namespace" {
  component "Ingress\n(one host, TLS)" as ingress
  component "Service api\n(http, metrics)" as svcapi
  component "Service web" as svcweb
  component "ServiceMonitor" as monitor
  node "Deployment api\n(2 replicas)" as api
  node "Deployment web\n(nginx, 2 replicas)" as web
  node "Deployment render\n(optional)" as render
  node "Deployment worker\n(exactly 1 replica)" as worker
  component "Jobs: secrets, roles,\nmigrate, seed" as jobs
}

database "Postgres\n(CNPG Cluster, bundled\nStatefulSet or external)" as pg
database "Valkey\n(bundled or external)" as valkey
database "S3 bucket\n(or attachments volume)" as s3

client --> ingress
ingress --> svcapi : /api, /.well-known/oauth-*
ingress --> svcweb : /
monitor -right-> svcapi : scrapes metrics
svcapi --> api
svcweb --> web
api -left-> render : PDF
api -[hidden]right- worker
web -[hidden]down- worker
api --> pg
api --> valkey
api --> s3
worker --> pg
worker --> valkey
worker --> s3
jobs --> pg : roles, migrations
@enduml
```

## Signing in with a password

A password sign-in posts JSON to `/api/v1/auth/login`; the api brakes guesses by the address guessed at and checks
an argon2id hash read through the admin database role, the only one that reads the secret tables. When the person
has an authenticator, the answer is a challenge and the session opens only at `/auth/login/second-factor`. The session
is a random secret stored as a digest in `user_session` and handed to the browser as the `armature_session` cookie.
There is no CSRF token: a write that rides on the cookie must be JSON, which a cross-site form cannot send, and in
production must come from Armature's own origin.

```plantuml
@startuml
title Signing in with a password
actor "Person" as person
participant "web" as web
participant "api" as api
database "Postgres" as db

person -> web : email and password
web -> api : POST /api/v1/auth/login (JSON)
api -> api : brake guesses for that address
api -> db : read the argon2id hash (admin role)
alt an authenticator is set up
  api --> web : 202 {challenge}
  person -> web : code from the app
  web -> api : POST /api/v1/auth/login/second-factor
  api -> db : use the challenge step once
end
api -> db : INSERT user_session (digest of the secret)
api --> web : 200 {principal}\nSet-Cookie armature_session\n(HttpOnly, SameSite=Lax, Secure)
web -> api : later writes carry the cookie
api -> api : refuse unless JSON and our Origin
@enduml
```

## Signing in through the identity provider

Single sign-on starts at `/api/v1/auth/oidc/start`, at the installation's one provider, which the suite runs as the
Keycloak realm `dynamo` with a client per tool. The state and nonce are kept in `oidc_login` and consumed on the
callback, so a code and state pair works once. The api reaches Keycloak at its in-network address while asking for the
public one, through `ARMATURE_OIDC_BACKCHANNEL`. Authenticating is not being let in: a person nobody admitted gets a
join request rather than a session, and their provider groups are replaced at every sign-in.

```plantuml
@startuml
title Signing in through the identity provider
actor "Browser" as browser
participant "api" as api
database "Postgres" as db
participant "Keycloak\n(realm dynamo)" as kc

browser -> api : GET /api/v1/auth/oidc/start?next=...
api -> db : INSERT oidc_login (state, nonce, redirect)
api --> browser : 302 to the provider
browser -> kc : sign in
kc --> browser : 302 /api/v1/auth/oidc/callback?code&state
browser -> api : the callback
api -> db : DELETE oidc_login by state, returning nonce
api -> kc : exchange the code (backchannel address)
kc --> api : ID token
api -> api : verify signature, issuer, audience, nonce
alt the person is admitted
  api -> db : replace their provider groups
  api -> db : INSERT user_session
  api --> browser : Set-Cookie armature_session, 302 to next
else nobody let them in
  api -> db : note a request to join
  api --> browser : back to sign in, with the reason
end
@enduml
```

## Calling the API with a token

A request authenticates with `Authorization: Bearer` before the cookie is looked at. A personal access token
(`armature_pat_`) is made from a session only, read-only or full, optionally confined to named projects; a connected
app holds an hour-long access token (`armature_oat_`) from Armature's own OAuth server. Every secret is stored as a
digest. The permission set is built once per request from the person's live role and then narrowed: a key drops what
only a global administrator holds and keeps to its projects, an app keeps only its scopes, and a read token's writes
are refused in middleware before any handler.

```plantuml
@startuml
title Calling the API with a token
participant "Client" as client
participant "api middleware" as mw
database "Postgres" as db
participant "handler" as handler

== Making a key ==
client -> mw : POST /api/v1/tokens (session only)
mw -> db : store the digest of armature_pat_...
mw --> client : the secret, shown once
== Using it ==
client -> mw : Authorization: Bearer armature_pat_ or armature_oat_
mw -> db : find the token by digest, then its person
mw -> db : resolve the person's permissions (live role)
mw -> mw : key: drop global admin, keep named projects
mw -> mw : connected app: keep only its scopes
alt a read token asks to write
  mw --> client : 403, before any handler
else allowed
  mw -> handler : request with the narrowed set
  handler --> client : answer
end
@enduml
```

## Reaching the suite

Armature calls no partner's API: Exciter and Stator call Armature as a person, and Armature sends them signed webhooks.
Both read `GET /api/v1/meta` first and use a feature only when its capability, such as `remote-links@1`, is listed.
Exciter gets each person's token by trading their Keycloak access token at `/oauth/token`, or the person pastes a
personal access token, and puts a remote link on every issue a requirement traces to. Stator connects through the
consent page with PKCE and makes its own webhook endpoint with `webhooks.partner`; Exciter's endpoint is one an
administrator adds by hand.

```plantuml
@startuml
title Reaching the suite
participant "Exciter" as exc
participant "Stator" as sta
participant "Armature api" as api
participant "Armature worker" as worker

note over exc, api : Armature API paths are under /api/v1
exc -> api : GET /meta
api --> exc : capabilities, e.g. remote-links@1
sta -> api : GET /meta
api --> sta : capabilities, e.g. oauth.partner-tokens@1
== A person's token ==
exc -> api : POST /oauth/token, token exchange\nof the person's Keycloak token
api --> exc : armature_oat_ and armature_ort_\n(or the person pastes an armature_pat_)
sta -> api : consent page /oauth/authorize, PKCE S256,\nthen POST /oauth/token
api --> sta : access and refresh token
sta -> api : POST /webhooks/partner (its own endpoint)
== Working as that person ==
exc -> api : GET /issues, POST /issues/{issueKey}/remote-links
sta -> api : GET /issues/{issueKey} for embeds, remote links
== Events back ==
worker -> exc : POST /api/v1/armature/webhook\nissue.* topics, X-Armature-Signature-256
worker -> sta : POST /api/v1/armature/webhook\nthe topics it subscribed to
@enduml
```

## Events from the outbox to the worker

An event is a row in `outbox_event`, written in the transaction that made the change, so no event describes a change
that rolled back. The worker's relay moves committed rows onto the Valkey stream `armature:events` and trims it only
below what every consumer group has acknowledged. Each group acknowledges an event once it is handled; after five
attempts the event is set aside as `event.dead_lettered` in the audit log. Delivery is at least once, so each consumer
is idempotent by the event's id.

```plantuml
@startuml
title Events from the outbox to the worker
skinparam componentStyle rectangle
left to right direction

component "api request" as api
database "outbox_event\n(same transaction)" as outbox
component "relay" as relay
queue "Valkey stream\narmature:events" as stream

package "worker consumer groups" {
  component "notify\n(inbox, mail)" as notify
  component "webhooks\n(signed deliveries)" as webhooks
  component "automation\n(rules)" as automation
  component "audit\n(admin log)" as audit
  component "git-sync\n(pull requests)" as gitsync
  component "desk-mail\n(customers)" as deskmail
}

api --> outbox : INSERT with the change
relay --> outbox : read committed rows
relay --> stream : XADD, wait at the cap
stream --> notify
stream --> webhooks
stream --> automation
stream --> audit
stream --> gitsync
stream --> deskmail
@enduml
```

## Reads pinned to the caller's own writes

Writes go to the primary and report the write ahead log position they committed at. The api keeps the greatest
position per session or token in Valkey for a few seconds, so a load balancer can send the next request to any api
process. A read then goes to a replica that has replayed past that position, or to the primary, and never waits for
replication: a lagging replica costs latency, never a stale answer about the caller's own change.

```plantuml
@startuml
title Reads pinned to the caller's own writes
actor "Caller" as caller
participant "api" as api
database "Valkey\n(freshness positions)" as valkey
database "Postgres primary" as primary
database "replica" as replica

caller -> api : write
api -> primary : commit
primary --> api : WAL position
api -> valkey : keep the greater position (short TTL)
caller -> api : read
api -> valkey : position this caller needs
alt a replica has replayed past it
  api -> replica : query
else no replica is fresh enough
  api -> primary : query, never wait
end
api --> caller : answer that includes the write
@enduml
```

## Git hosts, branches and pull requests

A repository is connected to a git host, Gitea, GitHub or GitLab, and the host's webhook posts to
`/api/v1/git/webhooks/{repositoryID}`, proven by the repository's own secret rather than a session. A push links its
commits to issues by key or by a branch made from the issue, and smart commit commands act once. The way back runs on
the worker: when an issue moves, its `git-sync` group comments on the pull requests, so a transition never waits on a
host.

```plantuml
@startuml
title Git hosts, branches and pull requests
actor "Developer" as dev
participant "Git host" as git
participant "api" as api
database "Postgres" as db
participant "worker\n(git-sync)" as worker

dev -> api : make a branch from an issue
api -> git : create the branch
api -> db : branch row names the issue
dev -> git : push, open a pull request
git -> api : POST /api/v1/git/webhooks/{repositoryID}
api -> api : verify the repository's secret
api -> db : link commits, run smart commits once,\nwrite events
db -> worker : issue moved event
worker -> git : comment on the pull request
@enduml
```
