# API contract for M4: the Stator integration

The agreement between whoever builds the backend and whoever builds the web
client of #40, #41 and #42. As in `docs/api-contract-m1.md`, the route table in
`backend/internal/httpapi/operations.go` is the source of truth for paths,
fields and types, and this file says what they mean. Every operation below is
in the table with `pending: true` and answers 501 `not_implemented` until it is
built; whoever builds one removes the mark, routes its handler and keeps this
file true. The types live in `backend/internal/stator`.

All paths are under `/api/v1`. Every operation but the webhook needs somebody
signed in; "admin" is an administrator of the installation, and "view" and
"edit" are the requirement rights of #12. A requirement the caller may not
view is 404, never 403.

The Stator integration is the Armature integration of M2 again, pointed at a
wiki: `docs/api-contract-m2.md` settled connecting, calling as the viewer, the
SSRF guard, the cache, the stub, link sync and webhooks, and what is the same
here is said briefly with a pointer to it. What differs is said in full.
Everything said about Stator's API was read from Stator's `main` at 94546d1,
vendored at `api/stator/openapi.json` with `api/stator/SOURCE` by
`make stator-openapi REF=`.

## What Stator has, and what it is asked for

Stator already has everything #40 and #41 need: personal access tokens
(`stator_pat_`) and `GET /auth/me`, `GET /pages/{pageID}`, the quick search
`GET /search/quick`, `GET /meta` with `product` and `capabilities`, and
outgoing webhooks that an administrator of Stator points anywhere, with the
topics `page.published`, `page.moved` and `page.deleted`, signed with
`X-Stator-Signature-256` under a `stator_whs_` secret, in Armature's envelope.
Their payload names the page with its id, title, version, space and URL.

What it lacks is the way back, for #42. It is filed in Stator as its #419 (the
text is at the end), and #42 stays `blocked` until it ships:

- `GET`, `POST` and `DELETE /pages/{pageID}/remote-links`: links another
  product puts on a page (`url`, `title`, `source`, `iconUrl`), a `POST` with a
  `url` the page has already replacing that link, as Armature's do on issues.
  Whoever may edit the page puts and takes them, with their own token.
- A panel on the page listing them, by source, for whoever may read the page.
- The webhook topic `page.remote_link.removed`, with the page and the removed
  link's `id` and `url`, so a link a person took off stays off.
- `page.remote-links` among the capabilities `GET /meta` answers, so Exciter
  knows when to start (`stator.RemoteLinksCapability`).

## Migrations, packages and settings

| Range | Owner | Holds |
|---|---|---|
| 00600 to 00609 | connecting (#40) | `stator_connection` (one row for the installation: base URL, whether Stator takes remote links, the webhook secret sealed), `stator_token` (one row per person: the token sealed, its row id, whom it acts as, its status, when it was checked), their policies |
| 00610 to 00619 | page links (#41) | `stator_page_link` (a requirement, a relation, the page's id, space key and URL, its latest version stored for everyone, the version and revision last confirmed, broken and detached), its clearances on `trace_link_clearance`, and the suspect view's share |
| 00620 to 00629 | the way back (#42) | `stator_remote_link` (what the worker last did on a page per requirement), the outbox policy for `stator.links`, and the triggers that emit it |

The issues name 00500, 00510 and 00520; M3 took those, so each range moved up
by a hundred, and goose never meets a lower number after a higher one.

Packages: `internal/stator` (the client, the cache, link sync, the webhook
check), `internal/netguard` as it is, and `cmd/stator-stub` (#40).

New settings, in compose:

| Setting | Meaning |
|---|---|
| `EXCITER_STATOR_BACKCHANNEL` | `public-origin=reachable-origin` pairs, as `EXCITER_ARMATURE_BACKCHANNEL`: where this process reaches a Stator whose base URL people open under another name. |

`EXCITER_OUTBOUND_ALLOW` gains `stator-stub` in the stack.

### Events and the audit log

| Topic | When | Payload |
|---|---|---|
| `stator.links` | a page link made or removed, a new revision, a move or rekey, trash, restore and purge of a requirement with page links, written by the database (#42) | `requirementId`, `actorId`, and nothing else |

New audit actions: `stator.connected`, `stator.disconnected` (the connection
saved or removed, with the base URL, never the secret).

## Rules for every Stator call

As for Armature (`docs/api-contract-m2.md`, "Rules for every Armature call"):

- **As the viewer.** Every call carries the caller's own Stator token as a
  bearer, so Exciter never shows a page the viewer could not open in Stator.
  The worker acts as the person whose change it is working on (#42).
- **Status instead of errors for reads**: `ok`, `not_configured`,
  `not_connected`, `rejected` and `unreachable` (no answer within
  `stator.CallTimeout`, 5 seconds, a refused address, a 5xx or a 429).
- **Writes relay Stator's refusal** as 409 `stator_not_configured`, 409
  `stator_not_connected`, 409 `stator_rejected` and 502 `stator_unreachable`;
  a 401 from Stator is never relayed as 401.
- **The SSRF guard** of `internal/netguard`, with redirects checked again and
  the `Authorization` header never carried to another host; an answer is read
  up to `stator.MaxResponseBytes`, 4 MiB, since a page's body comes with it.
- **Local development.** The stack sets `EXCITER_OUTBOUND_ALLOW` to name
  `stator-stub` and
  `EXCITER_STATOR_BACKCHANNEL=http://localhost:${STATOR_STUB_PORT}=http://stator-stub:8080`
  for the api and the worker; #31's suite points both at a real Stator.

### The cache

Kept per person in Valkey, keyed by the id of their stored token row; every key
starts `exciter:stator:`.

| Key | Holds | Lives |
|---|---|---|
| `page:{pageID}`, a hash, field `{tokenRowID}` | the page or null | `PageCacheTTL`, 60 seconds |
| `search`, a hash, field `{tokenRowID}:{sha256 of q, space and limit}` | the picker's answer | `SearchCacheTTL`, 60 seconds |
| `event:{eventID}` | that a webhook event was acted on | `WebhookReplayWindow`, 24 hours |

Without `EXCITER_VALKEY_URL` nothing is cached; a Valkey error is a miss.
Webhooks clear a page's entry and every search (#41).

### The page's version, stored for everyone

As an issue's status category is (M2), the latest published version of a
linked page is stored on the link for everyone, so the suspect list, the grid,
coverage and baselines judge a link without asking Stator as each reader. It
is written as the admin role whenever anybody reads the page live and Stator
answers it, and from `page.published`. Nothing else of the page is stored but
its id, its space key and its URL: a reader who may view the requirement but
not the page learns no more than that a page is linked.

## #40 Connect a Stator instance

| Operation | Needs | Answers |
|---|---|---|
| `GET /stator/connection` | admin | `{connection}`, null when none |
| `PUT /stator/connection` | admin | `{connection}`; 422 on `baseUrl`, `webhookSecret` |
| `DELETE /stator/connection` | admin | 204 |
| `GET /stator/account` | signed in | `{account}` |
| `PUT /stator/account/token` | signed in with a session | `{account}`; 422 on `token`, 409 `stator_not_configured`, 502 `stator_unreachable` |
| `POST /stator/account/check` | signed in | `{account}` |
| `DELETE /stator/account/token` | signed in | 204, also when there was none |

Armature's #26, with these differences:

- **The address** is checked with `GET {baseUrl}/api/v1/meta`, which must
  answer `product` `stator`, in any case; Stator's `/meta` reports the
  integration features it offers, and `remoteLinks` records whether
  `page.remote-links` is among them. `POST /stator/account/check` and every saved address read it again. A
  page opens at `{baseUrl}/s/{spaceKey}/p/{pageID}`.
- **No organization to learn.** Stator is one installation per team, as
  Armature and Exciter are (#82); the address alone names it, so there is no
  `statorOrgId`. A new address forgets every stored token and remote link
  record in the same transaction.
- **A token** starts `stator_pat_` and is checked with `GET /auth/me`, which
  does not say whether the token may only read, so the account has no
  `readOnly`. It is
  sealed bound to the installation and the person, and an Exciter personal
  access token cannot store or remove one (403 `session_only`).
- **The webhook secret** starts `stator_whs_`: an administrator of Stator adds
  a webhook under its Webhooks settings with `webhookUrl`,
  `{EXCITER_APP_URL}/api/v1/stator/webhook`, and the topics in `webhookTopics`,
  and pastes the secret Stator shows once. The last topic is offered once
  Stator ships it.
- **The stub.** `cmd/stator-stub` serves `/meta`, `/auth/me`,
  `/pages/{pageID}`, `/search/quick` and, ahead of Stator, the remote links,
  with the same tenants picked by the token as `armature-stub`; a contract test
  holds what it serves to `api/stator/openapi.json`, the remote links excepted
  until Stator's document has them.
- **UI:** Settings, Stator for administrators, and Your account, Stator account
  for everybody, as Armature's pages.
- **As built** (#40):
  - Stator's `/meta` answers `product` `stator` in lower case, and names each
    capability with a version after an `@`, as its #409 has it
    (`page.remote-links@1`); `stator.Meta.Offers` matches the name under any
    version or none. The name itself is what #419 asks for, and follows
    Stator if it settles on another.
  - Stator tokens do have a read-only mark, which `/auth/me` does not report;
    the account page asks for a token without it, and a write such a token
    cannot make fails as any refusal does (#42).
  - Every save of the address reads `/meta` again. A new address that does
    not answer as a Stator is refused; the address already saved is kept when
    Stator does not answer for a moment, with what it offered last.
  - `remote_links` is written as the admin role, after an administrator's
    save or anybody's check, and `exciter_app` may not write it, so nobody
    claims the capability through SQL; a new address clears it in the same
    trigger that forgets the tokens.
  - Removing the connection forgets the tokens; the remote link records join
    them with #42, which makes them.
  - The stub serves `/meta` and `/auth/me` so far; `/pages/{pageID}` and
    `/search/quick` come with #41, and the remote links with #42, each with
    the issue that calls them. `PUT /_stub/capabilities` sets what `/meta`
    lists for every tenant at once, since `/meta` names none; the stub's
    address as a browser opens it is `EXCITER_STATOR_URL` in
    `.cache/stack.env`.
  - The client's backchannel rewriter moved to `netguard.Rewriting`, which
    the Armature client uses too.

## #41 Link requirements to Stator pages

| Operation | Needs | Answers |
|---|---|---|
| `GET /stator/pages?id=` | signed in | `{status, pages}`: `{id, page}` per id asked, page null when not readable; 422 past 50 ids or for one that is no id |
| `GET /stator/search?q=&space=&limit=` | signed in | `{status, pages}` for the picker |
| `GET /requirements/{reqKey}/stator-links` | view | `{links}` |
| `POST /requirements/{reqKey}/stator-links` | edit | 201 `{link}`; 409 `stator_*`, 409 when that page is already linked with that relation, 422 on `pageId` or `relation`, 502 |
| `DELETE /stator-links/{linkID}` | edit on its requirement | 204 |
| `POST /stator-links/{linkID}/clear` | edit on its requirement | `{link}`; 409 `revision_moved_on` when the page has a newer version than the one named, 422 on `comment` |
| `POST /stator/webhook` | the signature | 204; 401 `bad_signature`; 413 `too_large`; 422 `unknown_version` |

- **A link** is a requirement, a relation (`specifies`, `describes` or
  `relates`, read from the requirement: it is specified by, described by or
  related to the page) and a page id. The same page may be linked once per
  relation; a section takes no links. A page Stator deleted is `brokenAt`, and
  a link a person took off the page in Stator (#42) is `detachedAt`; both stay
  until somebody removes them.
- **Linking** reads the page as the linker: one they may not read is 422 on
  `pageId`, "Stator has no page you may read with that id". The link stores
  the page's space key, URL and version, and that version as seen.
- **The picker** is Stator's quick search, pages whose titles start with the
  words typed, within a space when one is named, with each page's path.
- **Chips** show the title, space, path and version, read live with the
  viewer's token through `GET /stator/pages`, from the cache and Stator; a
  viewer without a token sees the relation, the space key and a link to the
  page.
- **Suspect** (#36). A link to a page watches the page: a page specifies or
  describes the requirement, so the requirement answers to it, and not the
  other way round. The link is suspect while the page's stored version is
  newer than the one seen; it shows on the suspect list, the grid's filter,
  the specification's badges and coverage as #36's links do, and is cleared
  by naming the version read, with a comment, recorded beside the other
  clearances. The requirement's revision at the time is recorded with it.
- **Webhooks** as Armature's #29: the body read whole up to
  `stator.WebhookMaxBytes`, 1 MiB; `X-Stator-Signature-256` the HMAC-SHA256 of
  the body under the secret, `sha256=` and hex; an installation without a
  connection or secret answered as a wrong signature, timed the same; an event
  id remembered in Valkey once acted on. What each topic does
  (`stator.WebhookTopics`):

  | Topic | Does |
  |---|---|
  | `page.published` | clears `page:{id}` and `search`; stores the version on the page's links |
  | `page.moved` | clears `page:{id}` and `search`; stores the new space key and URL |
  | `page.deleted` | clears `page:{id}` and `search`; marks the links `brokenAt` |
  | `page.remote_link.removed` | for a `url` under `{EXCITER_APP_URL}/r/`, marks the link `detachedAt` (#42) |
  | `ping` | nothing |

  Any other topic is 204 and ignored. Stator reads a payload as the webhook's
  owner and withholds what they may not read, so the Stator administrator who
  adds the webhook should read every space with requirements' pages.
- **Coverage** (#38) counts nothing from pages: a page explains a requirement,
  it neither decomposes, implements nor verifies it.
- **UI:** a "Stator pages" panel on the requirement page, beside the trace
  links and the Armature issues, with the picker, the relation and each link's
  chip, suspect badge and check.
- **As built** (#41):
  - Migration 00610. `stator_page_link` takes its requirement's revision as
    `reviewed_rev` in a trigger, which also refuses a section, a trashed
    requirement and an installation without Stator. `exciter_app` inserts
    the requirement, relation, page, space key, URL and itself, and nothing
    else: `page_version` and `page_version_seen` are written as the admin
    role, from a live read (`noteVersions`, which never lowers a version and
    gives a link that has seen none the one read) or a webhook, and the
    version seen moves on only through `stator_page_link_clear`. Clearances
    go into `trace_link_clearance` with `stator_link_id` and `page_version`
    beside the requirement's revision as `source_rev`.
  - The picker answers `StatorPageHit` (`id`, `title`, `spaceKey`,
    `spaceName`, `path`, `url`) rather than a whole page, since Stator's
    quick search has no versions. A link's `sync` is `none` until #42.
  - Clearing names the page's version: newer than the stored one, or below
    1, is 422 on `pageVersion`; older is 409 `revision_moved_on`.
    `link.cleared` carries `statorLinkId`.
  - `suspectLinks` (#36) counts Stator links too, so the grid's filter and
    the specification's badges show them; as with Armature links, the
    project's suspect list is of trace links only, and coverage counts
    nothing from pages. A broken or detached link is not suspect.
  - Baselines (#37) do not record Stator links yet, as they record
    Armature links in `baseline_external_link`; that is left for its own
    issue.
  - Webhooks act on `page.published`, `page.moved` and `page.deleted`; a
    page's address is built from the connection's base URL, not the one
    Stator writes in the payload. Stator's envelope carries no `version`,
    which reads as 1. Its document still requires `orgId` on
    `WebhookEnvelope`, which its webhook service no longer sends, so the
    stub sends none and its contract test checks the envelope's fields
    itself.
  - The stub reads pages and searches in Stator's shapes, with spaces ENG
    (everybody) and SEC (`admin` alone), and its controls publish, move and
    trash a page, each announced to the webhook a tenant registered, as
    `armature-stub` does.

## #42 Show Exciter links on Stator pages

No operations of its own; `GET /requirements/{reqKey}/stator-links` gains each
link's `sync` and `error`. Blocked until Stator ships what is asked of it
above.

- **Link sync** as Armature's #27: every change that can change what a page
  should show writes `stator.links` `{requirementId, actorId}` in its own
  transaction, from database triggers: a link made or removed, a new revision,
  a move to another project or a new project key, trash, restore and purge.
  The worker reads the requirement when it runs and posts, for each live link,
  `POST /pages/{pageID}/remote-links` with `url` `{EXCITER_APP_URL}/r/{requirementId}`,
  `title` `{KEY} {title} ({relation})` cut to `stator.MaxLinkTitleLength`,
  `source` `Exciter` and `iconUrl` `{EXCITER_APP_URL}/favicon-32.png`. A
  trashed or purged requirement, or a removed link, takes it off with
  `DELETE`, a 404 counting as done. One remote link stands per requirement and
  page, whatever the relations.
- **As whom.** The event's actor, with their own token; a link appears on a
  page only when somebody who may edit it there made one.
- **Before Stator can.** While the connection's `remoteLinks` is false the
  worker marks nothing pending and the links' `sync` is `none`; the next
  check that finds the capability starts a resync of every link.
- **Failures and removal** as #27: an unreachable Stator retries through the
  outbox, a refusal another try would meet again marks the link `failed` with
  a sentence, and a link a person took off in Stator stays off.

## Stator issue

Filed as `praetorianer777/Stator#419`, as agreed for #39:

> **feat: remote links on pages, for Exciter and other tools**
>
> Exciter traces requirements to the Stator pages that specify or describe
> them, and wants each page to show the requirements linked to it, as
> Armature's issues show Exciter's and Stator's links today.
>
> - `GET /pages/{pageID}/remote-links`, `POST /pages/{pageID}/remote-links`
>   and `DELETE /pages/{pageID}/remote-links/{linkID}`: a link has `url`,
>   `title` (at most 255 characters), `source` and an optional `iconUrl`, and
>   a `POST` with a `url` the page has already replaces that link. Reading
>   needs view of the page; putting and taking needs edit, and the link
>   records who put it.
> - A panel on the page listing its remote links, grouped by source, for
>   whoever may read the page.
> - The webhook topic `page.remote_link.removed` with the page and the removed
>   link's `id` and `url`, sent whoever removed it.
> - `page.remote-links` among the capabilities of `GET /api/v1/meta`.
>
> Exciter's side is its #42; its contract is `docs/api-contract-m4.md`.
