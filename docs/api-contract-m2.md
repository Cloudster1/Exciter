# API contract for M2: the Armature integration

The agreement between whoever builds the backend and whoever builds the web
client of #26, #27, #28, #29, #30 and #31. As in `docs/api-contract-m1.md`, the
route table in `backend/internal/httpapi/operations.go` is the source of truth
for paths, fields and types, and this file says what they mean. Every
operation below is in the table with `pending: true` and answers 501
`not_implemented` until it is built; whoever builds one removes the mark,
routes its handler and keeps this file true. The types live in
`backend/internal/armature`.

All paths are under `/api/v1`. Every operation but the webhook needs a member
of the organization; "admin" is an owner or administrator of the
organization, and "view" and "edit" are the requirement rights of #12. A
requirement the caller may not view is 404, never 403.

It follows Stator's integration with Armature (`docs/api-contract-m3.md` at
cf0f6f7), which settled the same questions for a wiki: what is the same here
is said again briefly, and what differs is said in full. Everything said about
Armature's API was read from Armature's `main` at fb100a6 and vendored again
at 7920a98, after its one-installation work (#87), at
`api/armature/openapi.json` with `api/armature/SOURCE`. What Exciter would like
from Armature and does not need is listed at the end.

## Migrations, packages and settings

| Range | Owner | Holds |
|---|---|---|
| 00300 to 00302 | connecting (#26) | `armature_connection` (one row for the installation: base URL, the Armature organization's id as learned, the webhook secret sealed; the slug went in 00403, #87), `armature_token` (one row per member: the token sealed, its row id, whom it acts as, its status, when it was checked), their policies |
| 00310 to 00319 | trace to issues (#27) | `armature_issue_link` (a requirement, a relation and an issue key, the status category stored for everyone, the remote link's id and sync state, broken and detached), the outbox policy for `armature.links`, and the triggers that emit it |
| 00420 to 00429 | create issues (#28) | nothing is planned; kept for what building it finds |
| 00430 to 00439 | webhooks (#29) | nothing is planned: replay protection and the cache live in Valkey, and the link columns are #27's |
| 00440 to 00449 | theme (#30) | following on `user_theme`, and the mirror columns on `theme` |

The last three ranges moved from 00320, 00330 and 00340 when the
one-installation work (#82) took 00400 to 00419, so goose never meets a lower
number after a higher one.

Packages: `internal/armature` (the client, the cache, link sync, the webhook
check, the theme mirror), `internal/netguard` (Armature's, credited in
`NOTICE`), and `cmd/armature-stub` (#26).

New settings, in compose:

| Setting | Meaning |
|---|---|
| `EXCITER_OUTBOUND_ALLOW` | Host names and CIDRs, comma separated, that the SSRF guard lets through although they are inside the network, as Armature's `ARMATURE_OUTBOUND_ALLOW`. Empty by default. |
| `EXCITER_ARMATURE_BACKCHANNEL` | `public-origin=reachable-origin` pairs, as `EXCITER_OIDC_BACKCHANNEL`: where this process reaches an Armature whose base URL people open under another name. |

### Events and the audit log

| Topic | When | Payload |
|---|---|---|
| `armature.links` | a link made or removed, a new revision, a move or rekey, trash, restore and purge of a requirement with links, written by the database (#27) | `requirementId`, `actorId`, and nothing else |

New audit actions: `armature.connected`, `armature.disconnected` (the
organization's connection saved or removed, with the base URL and slug, never
the secret).

## Rules for every Armature call

- **As the viewer.** Every call to Armature carries the caller's own personal
  access token as a bearer, so Exciter never shows an issue the viewer could
  not open in Armature. There is no shared token, for reads or for writes. The
  worker acts as the person whose change it is working on (#27).
- **Status instead of errors for reads.** A read that cannot ask Armature
  answers 200 with `status` saying why: `not_configured` (no connection),
  `not_connected` (the caller stored no token), `rejected` (Armature answered
  401 to the stored token; it is not used again until its owner stores a new
  one or checks it), `unreachable` (no answer within `armature.CallTimeout`,
  5 seconds, an address the guard refused, a 5xx or a 429). A requirement with
  ten linked issues then draws ten keys, not ten errors.
- **What a viewer without a token sees.** A link shows its key, its relation
  and the stored status category, linked to the issue in Armature, with
  "Connect your Armature account to see this issue" and a button to the
  profile. Without a connection there are no links to show.
- **Writes relay Armature's refusal.** Linking (#27) and creating (#28) answer
  409 `armature_not_configured`, 409 `armature_not_connected`, 409
  `armature_rejected`, 502 `armature_unreachable`, and otherwise Armature's own
  status with its sentence. A 401 from Armature is never relayed as 401.
- **The SSRF guard.** Every outbound call goes through `internal/netguard`,
  Armature's: the dialer resolves the host, refuses loopback, private,
  link-local, carrier grade NAT, multicast and metadata ranges unless
  `EXCITER_OUTBOUND_ALLOW` names the host or its address, and dials the address
  it checked. Redirects are followed at most three times, each checked again,
  and the `Authorization` header is not carried to another host. An answer is
  read up to `armature.MaxResponseBytes`, 4 MiB.
- **Local development.** The compose stack sets
  `EXCITER_OUTBOUND_ALLOW=armature-stub` and
  `EXCITER_ARMATURE_BACKCHANNEL=http://localhost:${ARMATURE_STUB_PORT}=http://armature-stub:8080`
  for the api and the worker. A real Armature on the host takes
  `host.docker.internal` the same way; #31 wires the three products together.

### The cache

As Stator's: Armature's answers are kept per person in Valkey, keyed by the
id of their stored token row, which is new each time a token is stored. Every
key starts `exciter:armature:` (#107 dropped the organization from it).

| Key | Holds | Lives |
|---|---|---|
| `issue:{KEY}`, a hash, field `{tokenRowID}` | the issue or null | `IssueCacheTTL`, 60 seconds |
| `search`, a hash, field `{tokenRowID}:{sha256 of q and limit}` | the picker's answer | `SearchCacheTTL`, 60 seconds |
| `meta:{tokenRowID}` | projects with `canCreate`, and issue types | `MetaCacheTTL`, 5 minutes |
| `theme:{tokenRowID}` | the id and `updatedAt` of the Armature theme, or null | `ThemeCacheTTL`, 5 minutes |
| `event:{eventID}` | that a webhook event was acted on | `WebhookReplayWindow`, 24 hours |

Without `EXCITER_VALKEY_URL` nothing is cached and every view asks Armature; a
Valkey error is a miss. Webhooks clear entries (#29); nothing else does.

### The status category, stored for everyone

`docs/decisions.md` records that the status category of a linked issue is
stored on the link, for everyone: coverage (#38) and baselines count it
without asking Armature as each reader, and a reader without a token still
sees whether the work is done. It is written:

- whenever anybody reads the issue live and Armature answers it, as a lookup,
  a link's chips or the worker's sync;
- from `issue.transitioned` once Armature sends `toStatusCategory` (see the
  end); until then a transition clears the cache and marks the stored
  category `statusStale`, which the next live read clears.

## #26 Connect an Armature instance

| Operation | Needs | Answers |
|---|---|---|
| `GET /armature/connection` | admin | `{connection}`, null when none |
| `PUT /armature/connection` | admin | `{connection}`; 422 on `baseUrl`, `webhookSecret` |
| `DELETE /armature/connection` | admin | 204 |
| `GET /armature/account` | member | `{account}` |
| `PUT /armature/account/token` | member, signed in with a session | `{account}`; 422 on `token`, 409 `armature_not_configured`, 502 `armature_unreachable` |
| `POST /armature/account/check` | member | `{account}` |
| `DELETE /armature/account/token` | member | 204, also when there was none |

- **The connection** is the base URL people open Armature at (absolute `http`
  or `https`, no user, query, fragment or path; `https` outside development
  unless `EXCITER_OUTBOUND_ALLOW` names the host). An Armature installation is
  one team's tracker, so the address is what names it (#87). Saving a new or changed address fetches
  `GET {baseUrl}/api/v1/openapi.json` through the guard and the backchannel,
  and refuses it on `baseUrl` unless `info.title` is `Armature`. An issue opens
  at `{baseUrl}/issues/{KEY}`.
- **The webhook secret** (`armature_whs_`, shown once by Armature) is sealed
  with `EXCITER_SECRET_KEY`, bound to the organization, and never answered;
  `webhookSecretSet` says whether there is one. Absent keeps it, empty removes
  it. `webhookUrl` is `{EXCITER_APP_URL}/api/v1/armature/webhook`
  and `webhookTopics` what to subscribe it to (#29).
- **A new address forgets every stored token** in the same
  transaction, and the link records' remote link ids, so an administrator can
  never send the members' tokens to a host of their choosing. Removing the
  connection does the same; the links themselves stay, keyed by issue.
- **`armatureOrgId`** is learned from the first token that checks out and kept
  until the address changes; every later token must come from the same
  Armature organization, while Armature still reports one on `/auth/me`. A
  token naming none has the address alone to go by. `connected` counts the members
  with a stored token.
- **A token** is made by the person in Armature and pasted under Your
  account, Armature account. Storing it asks `GET /auth/me` with it: a 401, a token of another
  Armature than the one learned, or one that does not start with `armature_pat_` is
  422 on `token` with a sentence saying how to make one. Armature's `/auth/me`
  answers the token's `scopes`: a token of the `read` scope is stored, and
  `account.readOnly` says it will not link or create (#27, #28). It is sealed
  with the context binding it to the organization and the person, and goes
  with the membership. An Exciter personal access token cannot store or remove
  one (403 `session_only`).
- **`account`** says whether the organization is `configured`, where issues
  open, whether the caller is `connected`, the `status` of the last check,
  whom the token acts as (`user`), `readOnly` and `checkedAt`. The database
  lets `exciter_app` read and write only the actor's own token row, and only
  administrators the connection.
- **Check** asks `GET /auth/me` now and stores `ok`, `rejected` or
  `unreachable`.
- **What later issues build on.** `armature.Service.Viewer(ctx)` is the person
  ctx acts for, with their `TokenID` and a `Caller` that makes every call with
  their token, or the status saying why there is none. A 401 from any call
  marks the token `rejected`.
- **UI:** Settings, Armature for administrators (address, organization, the
  webhook's address, topics and secret, with the steps in Armature), and
  Your account, Armature account for everybody.
- **As built** (#26):
  - The account is a page of its own at `/settings/armature-account`, reached
    from the account menu beside the access tokens, since Exciter has no
    profile page; the connection is at `/settings/armature`, among the
    settings. Both are Stator's components otherwise.
  - A read-only token stays offered for replacement: the page warns that it
    cannot create issues or links and keeps the token field open.
  - Armature knows one scope, `read`; a token without it may do whatever its
    owner may, so the stub answers `scopes: []` for everybody but `reader`.
  - The stack and the integration suite share `EXCITER_SECRET_KEY` through
    `mk/stack.mk`, so the stack's worker can open the tokens the suite stores
    once #27 syncs links. The stub's address as a browser opens it is
    `EXCITER_ARMATURE_URL` in `.cache/stack.env`.

## #27 Trace requirements to Armature issues

| Operation | Needs | Answers |
|---|---|---|
| `GET /armature/issues?key=` | member | `{status, issues}`: `{key, issue}` per key asked, issue null when not visible; 422 past 50 keys or for a key of the wrong shape |
| `GET /armature/issues/{issueKey}` | member | `{status, issue}` |
| `GET /armature/search?q=&limit=` | member | `{status, issues}` for the picker |
| `GET /requirements/{reqKey}/armature-links` | view | `{links}` |
| `POST /requirements/{reqKey}/armature-links` | edit | 201 `{link}`; 409 `armature_*`, 409 when that issue is already linked with that relation, 422 on `issueKey` or `relation`, 502 |
| `DELETE /armature-links/{linkID}` | edit on its requirement | 204 |

- **A link** is a requirement, a relation (`implements`, `verifies` or
  `relates`, read from the requirement: this requirement is implemented by,
  verified by, or related to the issue) and an issue key matching Armature's
  key shape, stored upper case. The same issue may be linked once per
  relation. A section takes no links. Nothing of the issue but its status
  category is stored.
- **Linking** asks Armature for the issue as the linker: an issue they may not
  see is 422 on `issueKey`, "Armature has no issue PROJ-12 you may see", so
  nobody links what they cannot open. A read-only token is 409
  `armature_rejected`.
- **The picker** asks `GET /armature/search`: a key finds that issue, words
  find the issues whose summary holds them (Armature's `GET /issues?text=`),
  at most `limit` (1 to 20).
- **Chips** show the key, summary, status and assignee, read live with the
  viewer's token through `GET /armature/issues?key=`, which answers from the
  cache and asks Armature for the rest; a viewer without a token sees the key,
  the relation and the stored category.
- **Link sync.** Every change that can change what the issue should show
  writes `armature.links` `{requirementId, actorId}` in its own transaction:
  a link made or removed, a new revision (the title may change), a move to
  another project or a new project key (the key changes), trash, restore and
  purge. The worker reads the requirement when it runs and puts, for each live
  link, Armature's `POST /issues/{KEY}/remote-links` with `url`
  `{EXCITER_APP_URL}/r/{requirementId}` (the permalink, which survives moves
  and rekeys), `title` `{KEY} {title} ({relation})` cut to Armature's 255
  characters, `source` `Exciter` and `iconUrl` `{EXCITER_APP_URL}/favicon-32.png`.
  A trashed or purged requirement, or a removed link, takes its remote link
  off with `DELETE /issues/{KEY}/remote-links/{id}`, a 404 counting as done.
  One remote link stands per requirement and issue, whatever the relations:
  its title names the first relation in the order above.
- **As whom.** The event's actor, with their own token; their name is what
  Armature records as the link's creator, and a link appears on an issue only
  when somebody who may edit it there made one.
- **Failures.** Unreachable, a timeout, 429 or a 5xx fails the event and the
  outbox retries it. A refusal another try would meet again (no token, a
  rejected or read-only token, 403, 404) marks the link `failed` with a
  sentence naming the actor, and the next event for the requirement tries it
  again. `sync` shows `synced`, `pending` or `failed`.
- **Detached and broken.** A link Armature took off the issue (#29) is
  `detachedAt` and never put back: "Armature removal wins". A deleted issue
  (#29) is `brokenAt`. Both stay on the requirement until somebody removes
  them.
- **Tools.** `list_armature_links` and `link_armature_issue` mark the list and
  the create operation once they are built.
- **UI:** an "Armature issues" panel on the requirement page, beside the trace
  links, with the picker, the relation and each link's chip and sync state.
- **As built** (#27):
  - The database writes `armature.links`, not each change's Go code: triggers
    on the links, on a requirement's revision, title, number, project and
    trash, on its deletion, and on a project's key call
    `armature_links_emit`, which writes one event per requirement and
    transaction, only for a requirement with links or sync records, only
    with a connection, and in the transaction's actor's name. A purge is the
    deletion trigger, so no `requirement.purged` topic was needed.
  - A lookup asks Armature for each key alone (`GET /issues/{KEY}`, eight at
    once) rather than one NQL search; the picker sends `text=` and `limit`.
    The stub serves `text` and `project` and refuses NQL.
  - The stored status category is written by the service as the admin role
    after any live read that answered the issue, so no reader can set it;
    `exciter_app` inserts a link without one.
  - Sync records live in `armature_remote_link`, one per requirement and
    issue, without a foreign key to the requirement so a purge still finds
    what to take off; a new or removed connection drops them.
  - A title too long for Armature keeps its key and relation and cuts the
    requirement's title, ending in three dots.
  - The worker needs `EXCITER_APP_URL`, which the links lead to.

## #28 Create Armature issues from requirements

| Operation | Needs | Answers |
|---|---|---|
| `GET /armature/projects` | member | `{status, projects}`: `{key, name, canCreate}` |
| `GET /armature/issue-types` | member | `{status, issueTypes}`, subtasks left out |
| `POST /projects/{projectKey}/armature-issues` | edit | 201 `{created, failed}`; 409 `armature_*`, 422, 502, and Armature's own refusals |

- **Projects** are Armature's `GET /projects`; `canCreate` comes from
  `GET /access/me` (`issue.write`). Both are cached as `meta`.
- **Creating** takes 1 to `armature.MaxCreateItems` (50) requirement keys of
  the project in the path, an Armature project, an issue type (Armature's
  default when none) and the relation, `implements` or `verifies`. One issue
  is filed per requirement, in order, with Armature's `POST /issues`: the
  summary is `{KEY} {title}` cut to 255 characters and the description one
  paragraph, "From {KEY} in Exciter", linked to the requirement's permalink.
  Each issue made is linked to its requirement at once, as #27's link, and
  synced. It stops at the first item Armature refuses: 201 with what was made
  and `failed: {requirementKey, code, message}`; when the first is refused,
  the refusal is the answer.
- **UI:** "Create Armature issues" in the specification view and the grid,
  for the requirements selected, with the project, type and relation.

## #29 Receive Armature webhooks

| Operation | Needs | Answers |
|---|---|---|
| `POST /armature/webhook` | the signature | 204; 401 `bad_signature`; 413 `too_large` |

- **Verification** as Stator's: the body is read whole up to
  `armature.WebhookMaxBytes`, 1 MiB; an installation without a connection or
  secret is 401 `bad_signature` like a wrong signature, with a
  stand-in secret checked so the timing is the same. `X-Armature-Signature-256`
  must be the HMAC-SHA256 of the body under the secret. Armature at fb100a6
  also sends `X-Armature-Timestamp` and `X-Armature-Signature-Timestamped-256`
  over `{timestamp}.{body}`: when they are present, the timestamped signature
  must hold as well. An envelope `orgId` other than the connection's
  `armatureOrgId` is 401.
- **Replays.** Armature resends the stored body on retries, so `occurredAt`
  and the timestamp are not judged by age; the event `id` is remembered in
  Valkey for `WebhookReplayWindow`, 24 hours, once acted on, and a second
  delivery is 204 and changes nothing.
- **What each topic does** (`armature.WebhookTopics`):

  | Topic | Does |
  |---|---|
  | `issue.updated` | clears `issue:{key}` and `search`; with `movedFrom`, rekeys the links of the old key and clears that entry too |
  | `issue.transitioned` | clears `issue:{key}` and `search`; stores `toStatusCategory` when the payload has one, else marks the links' category stale |
  | `issue.deleted` | clears `issue:{key}` and `search`; marks the links `brokenAt` |
  | `issue.remote_link.removed` | for a `url` under `{EXCITER_APP_URL}/r/`, marks the link `detachedAt` |
  | `issue.created` | clears `issue:{key}` and `search` |
  | `ping` | nothing |

  Any other topic is 204 and ignored. A database change a webhook makes is
  done as the admin role for the organization the signature vouched for,
  since no person acts.
- **UI:** the admin screen of #26 shows the address, the topics and the steps.

## #30 Follow my Armature theme

| Operation | Needs | Answers |
|---|---|---|
| `GET /armature/theme` | member | `{follow}`: `{following, status, error}` |
| `PUT /armature/theme` | member | `{follow}`; 409 `armature_*`, 422 `armature_theme_invalid` |
| `DELETE /armature/theme` | member | 204 |

Changed: `GET /themes/active` answers `source: "armature"` while the caller
follows Armature and Armature answered.

- As Stator's #34: following is kept on the person's `user_theme`, wins over
  the organization's default and ends when they choose a theme. The theme is
  Armature's `GET /themes/active` within `armature.ThemeTimeout`, 2 seconds,
  downloaded with `GET /themes/{id}/export`, checked as an `armature-theme/1`
  import and kept as the person's private mirror, downloaded again only for
  another id or a later `updatedAt`. Without a token, with a rejected one, or
  when Armature does not answer, `GET /themes/active` answers the fallback and
  `GET /armature/theme` says why.
- **UI:** "Follow my Armature theme" on the themes page, for whoever stored a
  token.

## #31 Run Exciter, Armature and Stator together

No operations. `make suite-up ARMATURE_DIR= STATOR_DIR=` starts the three
stacks with compose overrides kept in this repository, one Keycloak realm
`suite-dev` serving all three, fixed ports written down in `docs/suite.md`,
Armature's webhook pointing at Exciter, and connections from Exciter and Stator
to Armature made by seed steps. A smoke spec runs outside the gate. A change
Armature or Stator need goes through an issue in their repository, and this
issue is labelled `blocked` until it ships.

## The armature-stub

`cmd/armature-stub` stands in for Armature in the integration suite and in
Playwright, as `docs/decisions.md` says, ported from Stator's. It is a small Go
server in the backend image, a compose service `armature-stub` with its port
published as `ARMATURE_STUB_PORT`, the ninth of the checkout's block. #26
builds it, and each issue after adds what it calls: as #26 left it, it serves
`GET /openapi.json` and `GET /auth/me`, and under `/_stub/` a health check and
forgetting a tenant.

- **What it serves**, under `/api/v1`, answering exactly Armature's shapes:
  `GET /openapi.json` (the vendored document), `GET /auth/me` (with `token`),
  `GET /access/me`, `GET /projects`, `GET /issue-types`, `GET /issues`
  (`text`, `key in (...)` and `project =` are enough here), `GET /issues/{issueKey}`
  (with the old keys of moved issues), `POST /issues`, `GET` and
  `POST /issues/{issueKey}/remote-links`,
  `DELETE /issues/{issueKey}/remote-links/{remoteLinkID}`,
  `GET /themes/active` and `GET /themes/{themeID}/export`.
- **Who asks.** A token `armature_pat_{tenant}_{person}` is `person` in the
  stub's organization `tenant`, made on first use with fixed projects, types
  and issues; `reader` holds a token of the `read` scope, and
  `armature_pat_revoked` is always 401. Each Playwright spec uses a tenant
  of its own; since #83 they run one at a time, as the connection is the
  installation's.
- **Steering it** under `/_stub/`, left out of the contract test: change, move
  and delete an issue, set a person's theme, read the remote links it holds,
  make remote link calls fail for a while, and send a signed webhook to a
  given address and secret exactly as Armature signs one.
- **The contract test**, a Go unit test beside the stub and so in
  `make check-go`, loads `api/armature/openapi.json` and checks that every
  route the stub serves outside `/_stub/` is an operation of that document with
  the same method and path, drives each route, and validates every answer and
  every request body Exciter's client sends against the document's schemas with
  `internal/openapi`'s validator. `make armature-openapi REF=` vendors a new
  document; the test then says what the stub has to follow.

## What Exciter would like from Armature

None of these blocks M2, and Exciter works without each. Both are filed in
Cloudster1/Armature.

1. **Say what kind of thing a remote link is.** Armature's Pages panel lists
   every remote link of an issue as one list, so the requirements that an
   issue implements would sit among Stator's pages, told apart only by their
   source. Wanted: a `relation` (or `kind`) field on `RemoteLinkRequest`,
   `RemoteLink` and the remote link events, such as `page` or `requirement`,
   with `page` for links that send none, and the panel grouping by it under a
   heading each; or, smaller, the panel grouping by `source`. Exciter would
   then send `requirement` (#27). Filed as
   [Armature #223](https://github.com/Cloudster1/Armature/issues/223).
2. **Name the status category in transition payloads.** `issue.transitioned`
   names the statuses it moved between but not their categories, so a receiver
   that keeps the category has to ask for the issue again, as a person, which a
   webhook has none of. Wanted: `fromStatusCategory` and `toStatusCategory`
   beside `fromStatus` and `toStatus`. Exciter then stores it at once instead
   of marking it stale (#29). Filed as
   [Armature #224](https://github.com/Cloudster1/Armature/issues/224).
