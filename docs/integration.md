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
  "capabilities": ["requirement-embed@1", "specifications@1", "theme-export@1", "trace-links@1"]
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

### `trace-links@1`

The trace links between requirements: listed from either end, made with a link
type, and taken off.

- `GET /link-types`
- `GET /requirements/{reqKey}/links`
- `POST /requirements/{reqKey}/links`
- `DELETE /links/{linkID}`

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

The settings page and each member's account page list what is missing, each
as a sentence that names the capability, what goes without it, and to update
Armature. A connection saved before Exciter asked counts as offering
everything, so upgrading Exciter alone switches nothing off. Creating issues
(#28) has no capability in Armature's list yet, so it is not checked.

### Stator

Exciter reads Stator's `/meta` the same way, and takes remote links on pages
(#42) only once Stator lists `page.remote-links` (Stator #419), under any
version. Stator names no other capability yet (Stator #409), so nothing else
is checked; once it does, the page reads, search and webhooks check theirs as
Armature's do.

## Webhooks

Armature and Stator post their webhooks to Exciter in one envelope, which
names its version in the body and in a header:

```json
{ "version": 1, "id": "...", "topic": "issue.updated", "occurredAt": "...", "payload": { } }
```

| Sender | Address | Version header |
| --- | --- | --- |
| Armature | `POST /armature/webhook` | `X-Armature-Webhook-Version` |
| Stator | `POST /stator/webhook` | `X-Stator-Webhook-Version` |

- **Exciter reads version 1**, and an envelope without a version from a
  sender before versions: Armature before its #242, and Stator until its
  #412.
- **Any other version is refused**, from the header before the body is
  read, and from the body as well: `422 unknown_version`, with a warning in
  Exciter's log naming the version. The sender keeps the delivery and tries
  again, so nothing is misread or lost while one side catches up.
- **Unknown fields and topics are ignored**, answered 204.
- **The envelope is in the API document.** `ArmatureWebhookEnvelope` and
  `StatorWebhookEnvelope` in [`api/openapi.json`](../api/openapi.json)
  describe what Exciter reads, with the version header as a parameter. A
  unit test holds each to the sender's own schema in its vendored document,
  and the stubs' contract tests hold what they post to the senders' shapes.
- **Exciter sends in the same envelope.** Exciter sends no webhooks yet;
  when it does, they carry this envelope at version 1 and the header
  `X-Exciter-Webhook-Version`, and keep sending a version to the endpoints
  registered for it for at least one Dynamo release after a new one.

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
