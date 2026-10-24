# Integration capabilities

`GET /api/v1/meta` lists, under `capabilities`, the integration features this
build offers, as stable names with a version, such as `trace-links@1`. A
partner decides from these names whether a feature is there, not from the
version number of the build. `/meta` answers anyone, so a partner can ask
before it has an account; the names promise features, not data.

```json
{
  "product": "exciter",
  "build": { "version": "0.1.0", "commit": "abc1234", "builtAt": "2026-10-15T09:00:00Z" },
  "apiVersion": "1",
  "capabilities": ["mcp@1", "oauth.token-exchange@1", "requirement-embed@1", "requirement-list@1", "specifications@1", "theme-export@1", "trace-links@1", "webhooks@1", "webhooks@2"]
}
```

The list is sorted. Paths below are under `/api/v1`, spelled as in
[`api/openapi.json`](../api/openapi.json). Every operation takes a session or
an API token unless the document marks it public.

## What Exciter offers

### `requirement-embed@1`

A requirement read by its key, and requirements found by their words or the
first letters of their titles, with a session or a token.

- `GET /requirements/{reqKey}`
- `GET /search`
- `GET /search/quick`

### `requirement-list@1`

A project's requirements narrowed and ordered by a query in the query language
(#47), a window at a time, and what may come next at the caret of such a query.
A block on a partner's page that lists, say, `status = Approved ORDER BY key`
reads it here as the viewer, and its editor completes the query as the grid
does.

- `GET /projects/{projectKey}/requirements`, with `query`, `limit` and
  `offset`; a query that does not read is `400 bad_query` with the position
  of the character it went wrong at
- `GET /projects/{projectKey}/requirements/suggest`

### `trace-links@1`

The trace links between requirements: listed from either end, made with a link
type, and taken off.

- `GET /link-types`
- `GET /requirements/{reqKey}/links`
- `POST /requirements/{reqKey}/links`
- `DELETE /links/{linkID}`

### `mcp@1`

An assistant works with requirements over the Model Context Protocol, as the
person whose token it holds, in the revisions `2025-11-25` or `2025-06-18`
(#183). Only the endpoint is promised: the tools, resources and prompts are
what their lists name, and a partner reads them there.
[`docs/mcp.md`](mcp.md) says how to connect a client.

- `POST /mcp`

### `oauth.token-exchange@1`

A partner app trades a person's access token from the shared identity
provider for a token here (RFC 8693), so a tool beside Exciter reads it as
each person without anybody pasting a token (#273). The name and the promise
are Armature's (Cloudster1/Armature#302): `POST /oauth/token` with
`grant_type=urn:ietf:params:oauth:grant-type:token-exchange`, `subject_token`
an access token of the installation's OIDC provider whose `azp` is the app's
`keycloakClientId` and whose `aud` holds Exciter's client, the app
authenticating with its own id and secret; the answer an access and a refresh
token within the person's rights, with no consent page. An administrator
registers the app at `POST /oauth/clients`;
[`docs/api-contract-m7.md`](api-contract-m7.md), #273, has the rest.

The same grant type narrows a token too (#235): `subject_token` an access
token of the calling client's own grant answers an access token alone, no
wider than the grant in scope and projects (`exciter_projects`), ending at
the latest when its subject does (`exciter_expires_in`), the grant
untouched; #235 in the same document has the rest.

- `POST /oauth/token`

### `specifications@1`

The projects a caller may read, and each one's requirements as the outline of
its specification.

- `GET /projects`
- `GET /projects/{projectKey}`
- `GET /projects/{projectKey}/tree`

### `theme-export@1`

A theme exported as one `armature-theme/1` file with its pictures and fonts,
and such a file imported as a theme of the caller's own. The name and the
format are Armature's, so a theme travels between the three tools.

- `GET /themes/{themeID}/export`
- `POST /themes/import`

### `webhooks@1`

Endpoints subscribed to topics, their deliveries, a redelivery, a test
delivery and a new secret, each delivery a signed envelope. The name and the
operations are Stator's, whose webhooks Exciter's were adapted from (#48);
[Webhooks Exciter sends](#webhooks-exciter-sends) says what is posted.

- `GET /webhooks`
- `POST /webhooks`
- `PATCH /webhooks/{webhookID}`
- `DELETE /webhooks/{webhookID}`
- `GET /webhooks/{webhookID}/deliveries`
- `POST /webhooks/{webhookID}/deliveries/{deliveryID}/redeliver`
- `POST /webhooks/{webhookID}/test`
- `POST /webhooks/{webhookID}/rotate-secret`

### `webhooks@2`

What `webhooks@1` promises, and an endpoint registered for envelope version 2
is posted a CloudEvent 1.0 in structured mode (#133), as
[Webhooks Exciter sends](#webhooks-exciter-sends) says. The operations are
the same; `webhooks@1` stays listed beside it, since an endpoint on version 1
keeps getting version 1.

- `GET /webhooks`
- `POST /webhooks`
- `PATCH /webhooks/{webhookID}`
- `DELETE /webhooks/{webhookID}`
- `GET /webhooks/{webhookID}/deliveries`
- `POST /webhooks/{webhookID}/deliveries/{deliveryID}/redeliver`
- `POST /webhooks/{webhookID}/test`
- `POST /webhooks/{webhookID}/rotate-secret`

## What Exciter checks of its partners

### Armature

Exciter reads Armature's `/meta` when an administrator saves the connection,
also when the address stays the same, and when a member checks their token.
What it lists is kept with the connection, written only by the server's admin
role. Each feature checks for its capability by its exact name before it calls
Armature:

| Capability | What goes without it |
| --- | --- |
| `issue-embed@1` | Issue chips, the issue picker and linking an issue: reads answer `status: "outdated"`, a link is refused with `409 armature_outdated`. |
| `remote-links@1` | Putting requirements on their issues: the worker marks the link failed with the sentence, and the next change tries again. |
| `webhooks@1` | Keeping linked issues' statuses current; the settings say so, and Exciter still accepts what Armature sends. |
| `theme-export@1` | Following the theme Armature shows a person: following is refused with `409 armature_outdated`, and a follower keeps Exciter's theme with the sentence. |
| `remote-link-relations@1` | Telling an issue's requirements from its pages: links go without a relation, so Armature shows them as pages. Not among what Exciter needs, so nothing is listed as missing; the next sync after Armature lists it corrects the links. |

The settings page and each member's account page list what is missing, each
as a sentence that names the capability, what goes without it, and to update
Armature. A connection saved before Exciter asked counts as offering
everything, so upgrading Exciter alone switches nothing off. Creating issues
(#28) has no capability in Armature's list yet, so it is not checked.

### Stator

Exciter reads Stator's `/meta` the same way, and puts remote links on pages
(#42) only once Stator lists `page-remote-links@1` (Stator #419, since its
4afe5a1), under any version. The first check that finds it, a connection
saved or an account checked, puts every live page link on its page, each
requirement's as the maker of its newest link. Nothing else Stator lists is
checked yet; the page reads, search and webhooks are what every Stator has.

### Connecting people at sign-in

With the three tools on one Keycloak realm, Exciter connects each person to
Armature and Stator as they sign in (#274), so nobody pastes a token. After
an OIDC sign-in it trades the person's Keycloak access token for their tokens
at the partner, RFC 8693 as Armature's `docs/oauth.md` has it:

```
POST {baseUrl}/api/v1/oauth/token
Authorization: Basic <client id and secret, each form-encoded first>
Content-Type: application/x-www-form-urlencoded

grant_type=urn:ietf:params:oauth:grant-type:token-exchange
&subject_token=<the Keycloak access token>
&subject_token_type=urn:ietf:params:oauth:token-type:access_token
```

- **Only when switched on.** The operator sets
  `EXCITER_ARMATURE_TOKEN_EXCHANGE` and `EXCITER_STATOR_TOKEN_EXCHANGE`
  (Helm `armature.tokenExchange`, `stator.tokenExchange`); the partner lists
  `oauth.token-exchange@1`, by that exact name; and an administrator stores
  Exciter's client id and secret there on the connection,
  `PUT /api/v1/armature/connection` and `PUT /api/v1/stator/connection`
  with `clientId` and `clientSecret`. The secret is sealed with
  `EXCITER_SECRET_KEY` and never answered; the connection answers
  `clientId`, `clientSecretSet`, `tokenExchange` and `exchangeOffered`.
- **No scope is asked**, so the partner gives what its administrator allowed
  the app. Armature's token holds `read issue.write`; Exciter takes a token
  that holds `read` without `issue.write` for one that may only read.
- **The tokens sit where a pasted one does**, sealed per person and partner
  with the refresh token beside them, so every read and the link worker use
  them alike. A token due within a minute is refreshed where it is used,
  one refresh at a time under a row lock, as a rotated refresh token
  presented twice ends the grant; the worker making links while the person
  is away renews it the same way. `invalid_grant` on a refresh forgets the
  token, and the account says the partner ended the connection.
- **A token the person pasted and that works stays**, and the exchange is
  not tried; a pasted token replaces an exchanged one.
- **A failed exchange never fails the sign-in.** Each partner has at most
  three seconds, both at once; the account page says in a sentence why the
  person is not connected (`signIn` on `GET /armature/account` and
  `GET /stator/account`, and `source`: `none`, `token` or `sign_in`).

In the realm, Exciter's Keycloak client carries an audience mapper for each
partner's client (`armature`, `stator`), so its access tokens hold their ids
in `aud`; each partner registers Exciter as a connected app whose Keycloak
client is `exciter`.

A test result's commit links to Armature's development panel (#200) when an
issue the requirement links to names the commit there. The panel,
`GET /issues/{issueKey}/development`, has no capability in Armature's list, so
Exciter asks it for each linked issue as the reader and takes a 404 for a panel
that is not there.

## CI pipelines

A pipeline sends Exciter its test results and asks for a verdict with
`exciter-ci`, released as `ghcr.io/cloudster1/exciter-ci` (#200). It needs a
personal access token, or an app's grant, of the `evidence` scope, which
reaches the uploads and the trace check alone; make one under Your account,
Access tokens, with Evidence only.

| Variable | What it is |
| --- | --- |
| `EXCITER_URL` | Where Exciter is served, such as `https://exciter.example.com`. |
| `EXCITER_TOKEN` | The evidence token. |
| `EXCITER_PROJECT` | The project's key. |

The repository, commit, branch, job and run address come from GitHub Actions,
Gitea Actions or GitLab CI, a pull request's branch being its head; flags of the
same names set them anywhere else.

```yaml
- run: go test -json ./... | go-junit-report -parser gojson > junit.xml
- run: >
    docker run --rm -v "$PWD:/work" -e EXCITER_URL -e EXCITER_TOKEN -e EXCITER_PROJECT
    -e GITHUB_ACTIONS -e GITHUB_REPOSITORY -e GITHUB_SHA -e GITHUB_REF_NAME -e GITHUB_HEAD_REF
    -e GITHUB_SERVER_URL -e GITHUB_RUN_ID -e GITHUB_JOB
    ghcr.io/cloudster1/exciter-ci results junit.xml
- run: docker run --rm -e EXCITER_URL -e EXCITER_TOKEN -e EXCITER_PROJECT ghcr.io/cloudster1/exciter-ci check --fail broken,gaps,failing
```

- `exciter-ci results FILE...` uploads JUnit XML reports, a quoted pattern
  included, waits until the worker has landed them (`--wait`, ten minutes by
  default), and prints what was read and each key that verifies nothing.
- `exciter-ci check` prints the trace check's verdict and what failed it,
  narrowed by `--query` and judged on `--branch`, the project's counting
  branch when empty. `--fail` names what fails it.
- `exciter-ci scan [DIR]` (#201) reads the requirement tags in a checkout's
  source comments, `[implements CHK-R42]` or `[verifies CHK-R42@3]` in the
  project's tag words, and uploads them for its branch, replacing that branch's
  scan before. It reads the files git tracks when `DIR` is a checkout, every
  file but build output otherwise, and leaves out what `--exclude` names. The
  address of a line comes from GitHub, GitLab or Gitea, or `--file-url`; git
  inside a container needs the checkout marked safe, as `make code-scan` does.
- All three exit 0 when they pass, 1 when the check fails, and 2 when Exciter
  could not be asked, with the sentence it answered.

A test case names what it verifies with `[verifies KEY]` in its name, a
property named `verifies` or `requirement`, or a `tag` property such as
`verifies:KEY`. `make test-results`, `make code-scan` and `make trace-check`
run the same against this checkout's stack.

## Webhooks

Armature and Stator post their webhooks to Exciter in one envelope, which
names its version in the body and in a header. Version 1:

```json
{ "version": 1, "id": "...", "topic": "issue.updated", "occurredAt": "...", "payload": { } }
```

Version 2 is a CloudEvent 1.0 in structured mode, posted as
`application/cloudevents+json`, as agreed in Armature #286 and Stator #434
(#133). Its data is version 1's payload:

```json
{ "specversion": "1.0", "id": "...", "source": "https://armature.example.com", "type": "dev.armature.issue.updated",
  "time": "...", "datacontenttype": "application/json", "dataversion": 1, "data": { } }
```

| Sender | Address | Version header | Type prefix |
| --- | --- | --- | --- |
| Armature | `POST /armature/webhook` | `X-Armature-Webhook-Version` | `dev.armature.` |
| Stator | `POST /stator/webhook` | `X-Stator-Webhook-Version` | `dev.stator.` |

- **Exciter reads versions 1 and 2**, and an envelope without a version from
  a sender before versions: Armature before its #242, and Stator until its
  #412. Version 2's topic is its type after the sender's prefix, its payload
  its data, and its time when the event happened. Neither partner sends
  version 2 yet; Exciter reads it as agreed, and the stubs post it when a
  test registers for it.
- **The signatures are version 1's** in either version, over the body as
  sent, since CloudEvents leaves authentication to the transport.
- **Any other version is refused**: a version header, a body's version, a
  `specversion` other than `1.0` or a `dataversion` other than 1, and a
  header and a body that name different versions. Exciter answers
  `422 unknown_version` with a warning in its log naming the version. The
  sender keeps the delivery and tries again, so nothing is misread or lost
  while one side catches up.
- **Unknown fields, topics and types are ignored**, answered 204; a type
  with another product's prefix is no topic of the sender's.
- **The envelope is in the API document.** `ArmatureWebhookEnvelope` and
  `StatorWebhookEnvelope` in [`api/openapi.json`](../api/openapi.json)
  describe version 1, held by a unit test to the sender's own schema in its
  vendored document, and `PartnerCloudEvent`, under
  `application/cloudevents+json`, version 2, which neither partner's
  document holds yet. The stubs' contract tests hold what they post in
  version 1 to the senders' shapes.
- **Exciter sends in the same envelopes**, below.

## Webhooks Exciter sends

An installation's administrators keep endpoints under Settings, Webhooks
(#48): an address, the events it hears, the projects it listens to, every
project when none are named, and the envelope version it is posted in. Each
event is posted in version 1, as `application/json`, named `exciter` as its
source:

```json
{ "version": 1, "id": "...", "source": "exciter", "topic": "requirement.revised", "occurredAt": "...",
  "payload": { "requirement": { "key": "CHK-R4", "title": "...", "revision": 3, "url": "..." }, "revision": 3, "actor": { } } }
```

or, for an endpoint that chose version 2 (#133), as a CloudEvent 1.0 in
structured mode, `application/cloudevents+json; charset=utf-8`, its data
version 1's payload unchanged:

```json
{ "specversion": "1.0", "id": "...", "source": "https://exciter.example.com", "type": "dev.exciter.requirement.revised",
  "time": "...", "datacontenttype": "application/json", "dataversion": 1,
  "data": { "requirement": { "key": "CHK-R4", "title": "...", "revision": 3, "url": "..." }, "revision": 3, "actor": { } } }
```

- **`source` is the installation's address**, `EXCITER_APP_URL`, so a relay
  or a broker hearing several installations tells them apart; `type` is
  `dev.exciter.` and the topic; `dataversion` is the payload's own version,
  which rises only for a change that breaks a receiver of the data.
- **`actor.via`** says what the change was written through: `{kind, name}`,
  `token` and the personal access token's name, or `oauth` and the
  connected app's (#190). It is left out for a change made in the browser,
  so a receiver of either version that does not know it reads past it.
- **Version 1 is the default.** A new endpoint is on version 1 unless it
  names 2, in the form or through `version` on `POST /webhooks`; saving one
  without `version` keeps the one it has; the list shows each, and its menu
  switches it. Version 1 keeps being sent for at least one Dynamo release
  after version 2 ships, as the rules below say.

| Header | What it carries |
| --- | --- |
| `X-Exciter-Event` | The topic, as in the body |
| `X-Exciter-Delivery` | This attempt's id; a retry or a redelivery has its own |
| `X-Exciter-Webhook-Version` | The envelope version: 1, as in the body, or 2 for a CloudEvent |
| `X-Exciter-Signature-256` | `sha256=` and the hex HMAC-SHA256 of the body, keyed with the endpoint's secret |
| `X-Exciter-Timestamp` | When the attempt was sent, in Unix seconds |
| `X-Exciter-Signature-Timestamped-256` | The same over the timestamp, a dot and the body, so a receiver can refuse a replay |

- **The topics** are `requirement.created`, `requirement.revised`,
  `requirement.changed`, `requirement.trashed`, `requirement.restored`,
  `requirement.moved`, `link.created`, `link.removed`, `link.cleared`,
  `review.requested`, `review.decided`, `review.closed`, `comment.created`,
  `thread.resolved` and `baseline.taken`, and `ping` for a test. A receiver
  ignores a topic or a field it does not know.
- **The payload is read as the endpoint's owner**, the administrator who
  saved it last, when each attempt is sent: references to the requirement
  (key, title, revision, status, project, address), the actor, and what the
  event says. What the owner may not view is withheld, never sent.
- **The `id` is the event's**, the same on every attempt and redelivery, so a
  receiver can tell a repeat.
- **A receiver answers 2xx to take a delivery.** Anything else, or no answer
  within 10 seconds, is tried again after 1 minute, 5 minutes, 30 minutes, 2
  hours and 12 hours; an endpoint failing every attempt for a day is turned
  off until an administrator turns it on.
- **The envelope is in the API document** as the `event` webhook of
  [`api/openapi.json`](../api/openapi.json), with every payload and header:
  `WebhookEnvelope` under `application/json`, and `WebhookCloudEvent`, with
  every type, under `application/cloudevents+json`.
  Exciter keeps sending a version to the endpoints registered for it for at
  least one Dynamo release after a new one.
- **Nothing is posted into the server's own network** unless
  `EXCITER_OUTBOUND_ALLOW` names the host, checked again on every attempt.

## The rules

- **A name is promised for what it lists.** The operations keep their paths,
  their parameters and the fields they answer with; a field may be added, none
  is taken away or changes meaning.
- **A breaking change raises the number.** Renaming or removing one of the
  operations above, a field a partner reads, or a meaning, makes the
  capability `trace-links@2`. The old name is still reported beside the new
  one, and still kept, for at least one Dynamo release.
- **The list is shared.** The names and what each promises belong in one list
  in the Dynamo repository (Dynamo#4, not written yet). A tool may only report
  names from it; this page follows it once it is there.
- **The build is held to it.** The list lives in
  `backend/internal/httpapi/capabilities.go`. A unit test refuses a capability
  naming an operation the operations table does not have, or one only agreed
  and not built, and another refuses a name that is not `name@version` or is
  listed twice.
