# API contract for the backlog

Features planned after the first release, one section each, agreed as the
milestones' were (`docs/api-contract-m1.md`). The route table in
`backend/internal/httpapi/operations.go` is the source of truth for paths,
fields and types, and this file says what they mean. All paths are under
`/api/v1` and need somebody signed in; "view" and "administer" are the project
rights of #12, and a project the caller may not view is 404, never 403.

## Migrations, packages and settings

| Range | Owner | Holds |
|---|---|---|
| 00710 to 00719 | ReqIF (#44) | `reqif_identity` (what an identifier of an exchange names in a project), `reqif_import` (a file on its way in), `reqif_import_object` (the files its rich text shows, until they are attachments), their guards and policies |
| 00720 to 00729 | Documents (#45) | `api_token.for_render` and the check `api_token_render_is_brief`, which hold a print's token to reading for minutes |
| 00730 to 00739 | Signatures (#46) | `reauthentication` (a proof of identity given again, for one signature), `signature_failure` (wrong passwords, for the limit), `review_signature`, the purpose of an `oidc_login`, and the triggers `review_signature_guard` and `review_decision_signed` |
| 00740 to 00749 | Query language (#47) | `requirement_key_id` (the requirement a key, or one it had, names) and `requirement_coverage` (a requirement's level in one dimension, as `trace.Judge` judges it), both read as the reader sees |
| 00750 to 00759 | Webhooks (#48) | `webhook_endpoint` (an address, its events and projects, a sealed secret, its owner) and `webhook_delivery` (one attempt at one event), the trigger `webhook_endpoint_stamp` and their policies |
| 00760 to 00769 | Reuse (#49) | `requirement.reuse_source`, `reuse_revision` and `reuse_state`, the guard `requirement_reuse_guard`, the change field `reuse` and the notification kind `source_revised` |
| 01360 to 01369 | Suite demo (#270) | `suite_demo` (the one demo: its state, step and counts, and the snapshot and keys it was made with), `suite_demo_requirement` (which issue or criterion a requirement came from), the guard `suite_demo_guard` and their policies |

Packages: `internal/reqifio` (#44); `internal/specdoc`, `internal/docx` and `internal/render` (#45); `internal/review/signing.go`
and `internal/auth/reauth.go` (#46); `internal/rql` (#47); the sender in `internal/webhook` (#48); `internal/requirement/reuse.go` (#49). Worker topic:
`reqif.requested`. Settings: `EXCITER_RENDER_URL`, `EXCITER_RENDER_TIMEOUT` (20s), `EXCITER_RENDER_CONCURRENCY` (4) and
`EXCITER_RENDER_MAX_SIZE` (50MB) name and bound the render service; a blank URL turns PDF export off.

## #44 ReqIF import and export

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/reqif-imports` | administer | 201 `{import}`; 413 `too_large`; 422 |
| `GET /reqif-imports/{importID}` | its maker, or administer | `{import}` |
| `PUT /reqif-imports/{importID}/choices` | its maker | `{import}`; 409 when it already started |
| `POST /reqif-imports/{importID}/start` | its maker | 202 `{import}`; 409 `import_problems` |
| `DELETE /reqif-imports/{importID}` | its maker, or administer | 204 |
| `GET /projects/{projectKey}/export.reqif` | view | the document; 404 for a `section` the project lacks |
| `GET /projects/{projectKey}/export.reqifz` | view | the archive with the files the text shows |

- **Upload** takes a ReqIF document (`.reqif`, ReqIF 1.0.1 to 1.2, which share
  one namespace) or a `.reqifz` zip holding one, UTF-8: at most
  `reqifio.MaxImportBytes` (64 MB, else 413), a document of at most 32 MB
  unpacked, an archive of at most 128 MB unpacked, and
  `reqifio.MaxObjects` (5000) objects, else 422 with the reason under
  `file`. Tool extensions are passed over. The files of an archive the rich
  text shows, up to 50 MB each, are kept with the import. Nothing is written
  to the project.
- **The preview** says what the file holds (`objects`, `sections`,
  `relations`, `images`, its specifications and source tool), how many
  objects make a requirement (`new`) or change one (`updates`), and its
  `schema`: every SPEC-OBJECT-TYPE, ATTRIBUTE-DEFINITION and
  SPEC-RELATION-TYPE the objects and relations use, with what it becomes and
  `by` what:
  - `identifier`: an earlier import or export of this project tied it;
  - `name`: the project has a type, attribute (of a kind its values `fit`) or
    link type (by key or name) of its name;
  - `choice`: somebody mapped it;
  - `field`: an attribute that fills a field: `ReqIF.Name` the title,
    `ReqIF.ChapterName` a section's title, `ReqIF.Text` the statement,
    `ReqIF.ForeignID` nothing (the project gives keys), and an export's own
    rationale, status, priority, owner and labels;
  - `create`: the import makes it, a type with the default type's look, an
    attribute of the first kind that fits (text past 500 characters is rich
    text, an export's people and web addresses keep their kinds), or a link
    type of the project's own;
  - `skip`: left out; `section`: a headings' type and its attributes;
    `none`: nothing yet, and a problem says why.
- **Losses** are what the file holds that the project cannot keep, by
  object: formatting the editor has no form for (kept as its words), a file
  that did not come along, a value of the wrong shape, a relation to a
  section or to nothing, an object placed twice or never, a status an
  existing requirement keeps, a person who is no member. They do not keep the
  import from starting; at most `reqifio.MaxLosses` (200) are listed and
  `lostCount` counts them all.
- **Problems** keep it from starting: an attribute the project has by name
  whose kind does not fit, a choice naming nothing of the project, a text
  value past 500 characters for a text attribute, a required attribute
  without a value, too many options. `choices` maps a schema item's
  identifier to an id of the project's, `create` or `skip`; every choice is
  sent at once. `start` reads the project again and is refused with 409
  `import_problems` while any problem is left.
- **Objects** are requirements, or sections: of an export's section type, or
  with a `ReqIF.ChapterName` and no `ReqIF.Text`. A specification an export
  wrote is the project; any other becomes a section at the top named after
  it, found again by its identifier the next time.
- **The worker** runs a started import after `reqif.requested`, as its maker:
  - it makes the types, attributes, options and link types to make, widens
    the attributes it maps to so they show on the types that use them and
    hold the options the file names, and ties every identifier;
  - then each object in the order of the specifications, in its own
    transaction: a new one as revision 1, with the comment "Imported from
    <file>", at its place; one an identifier ties as its next revision,
    moved where the file has it unless it sits at the top of an export's
    specification or in none;
  - an object whose text shows files gets them as attachments, and a new
    one shows them in revision 2;
  - then the relations, as links, leaving the links the project has.
  `done` of `total` counts objects; `imported`, `updated` and `unchanged`
  say what became of each, and `refused` names the object and why.
  Cancelling stops it after the object it is on.
- **Export** writes the project, or with `section` a requirement or section
  and everything below it, up to `reqifio.MaxExportRows` (20000)
  requirements: a SPEC-OBJECT-TYPE per requirement type with the fields and
  the attributes that show on it, a section type, an enumeration per select
  attribute and for statuses and priorities, the tree as one specification,
  and the links between what it holds as relations. What an exchange tied
  keeps its identifier; the rest are `exciter-` and the row's id, which an
  import into the same project finds again. The archive packs the files the
  text shows as `files/<attachment id>.<ext>`, up to 128 MB; past either
  limit the answer carries `X-Export-Truncated: true`.
- UI: a ReqIF tab in the project settings, with the exports for whoever
  views the project and the import for its administrators, and a ReqIF
  download on every section of the specification view.
- **As built** (#44):
  - **What the database holds.** `reqif_identity` is read by whoever views
    the project and written by who administers it, and an identifier names
    only something of its own project, a specification only the project or
    one of its sections. An import is made by who administers its project,
    in their own name, and seen and stopped by them and its maker; its file
    never changes, its choices only until it is queued, and it moves only
    forward. Its files go with it.
  - **Files.** They wait in the database, not the bucket, so the API and the
    worker share nothing else. Without object storage an upload leaves them
    out and lists them as losses.
  - **Status** is set only on what the import makes, by
    `requirement_import_status`, as a CSV import does; a requirement that
    exists keeps its own.
  - **The schema** an export writes validates against the OMG ReqIF 1.2
    schema: `make reqif-schema` checks the golden export and the test file,
    with the schema and the W3C modules it includes fetched once and held to
    `api/reqif/schema.sha256`.

## #45 Export a specification to Word and PDF

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/document` | view | `{document}`; 404 for a `section` the project lacks |
| `GET /projects/{projectKey}/document.docx` | view | the Word file; 422 `docx_too_large` |
| `GET /projects/{projectKey}/document.pdf` | view | the PDF; 503 `render_unavailable` or `render_busy`, 504 `render_timeout`, 502 `render_failed` |
| `GET /baselines/{baselineID}/document` | view | `{document}` |
| `GET /baselines/{baselineID}/document.docx` | view | the Word file |
| `GET /baselines/{baselineID}/document.pdf` | view | the PDF |

- **The document** is a project's specification, with `section` a
  requirement or section and everything below it, or a baseline as it was
  taken. Its cover says what it is of, the baseline's day, maker and lock,
  who exported it and when, and how many entries it holds. Each entry is its
  outline number, key, revision, title, statement and rationale, a table of
  its type, status, owner, priority, labels and attribute values as a reader
  reads them (options by name, people by name, rich text as a document), and
  its links, read from it. A baseline's entries are at the revisions it holds,
  with the links it held, and without owner, priority or labels, which a
  baseline does not keep. At most `specdoc.MaxEntries` (1000) entries; past
  them the document says so.
- **Word** is written by the api: the cover, the contents linking to each
  entry, each entry under one of Word's heading styles, its pictures inside up
  to 50 MB (`docx_too_large` past them). Its words are the reader's language,
  English or German.
- **PDF** is the web client's print view, `/print/p/{projectKey}/document`
  or `/print/baselines/{baselineID}`, printed by the render service as the
  reader with a token made for that one print: read only, two minutes, listed
  nowhere, deleted once the PDF is back. A token held to some projects cannot
  print (403). Without a render service, 503 `render_unavailable` says to
  export Word instead.
- Every export is recorded as `specification.exported` before the file
  leaves; one the log cannot record is refused.
- UI: an Export menu, PDF and Word, on the specification, on every section
  there (with ReqIF), and on a baseline.
- **As built** (#45):
  - **Files are named** after what they hold and the day:
    `KEY-2026-10-15.pdf`, `KEY-R4-2026-10-15.docx` for a section,
    `KEY-release-1-2026-10-15.docx` for a baseline.
  - **The print reads fresh.** The print's token is noted at the position of
    its own making in read-your-writes, so its requests through the replica
    see what the reader wrote just before.
  - **What the database holds.** A print's token reads only, lasts at most
    five minutes and reaches every project its owner does, or the row is
    refused; the app changes no token at all.

## #46 Re-authenticate to sign an approval

| Operation | Needs | Answers |
|---|---|---|
| `PATCH /projects/{projectKey}` with `settings.signedApprovals` | administer | `{project}`; the change is audited as `project.signatures_set` |
| `POST /reviews/{reviewID}/decisions` with `password` or `reauthentication` | an approver, signed in | `{review}`; 422 on `password`, 403 `session_only`, 409 `signature_spent`, 429 `too_many_attempts` |
| `GET /auth/oidc/reauthenticate?next=` | signed in | a redirect to the identity provider, back to `next` with `signature` or `signature_error` |
| `GET /auth/me` | signed in | `signsWith`: `password`, `provider` or empty, for a session |

- **The setting** is a project's, off by default. With it on, approving and
  rejecting a revision are both electronic signatures; asking and withdrawing
  are not.
- **The meaning** is a sentence the server writes for each decision, in the
  reader's language: "I approve revision 3 of CHK-R1, Pay the basket, as one
  of its approvers." A review carries `signed` and, for someone who may
  decide, `meanings` by decision; the signature keeps the sentence as shown.
- **Proving who one is.** Every signature proves it again, and nothing
  carries over to the next. Somebody with a password types it with the
  decision; it is checked outside the transaction, and five wrong ones within
  fifteen minutes refuse the next until the window passes (429). Somebody who
  signs in at the identity provider goes there with `prompt=login`,
  `max_age=0` and their email as the hint; the callback checks the session is
  the one that left, the provider signed in the same person, and its
  `auth_time` is no older than the request, then records a re-authentication
  good for five minutes and one signature, and comes back to `next` with
  `?signature=<id>`, or `?signature_error=` `session`, `someone_else`,
  `not_fresh` or `unavailable`. A token never signs (403 `session_only`).
- **A signature** keeps the meaning, the decision, the revision and the hash
  of its normative content, how and when the signer proved who they are, and
  in which session. It never changes; a later decision of the same reviewer
  is a new signature. Each decision of a review shows its latest as
  `signature`.
- UI: a checkbox in the project settings; Approve and Reject open a dialog
  that shows the meaning, then asks for the password or sends the person to
  the identity provider, keeping their comment for the way back.
- **As built** (#46):
  - **The database proves it too.** `review_signature_guard` refuses a
    signature not given by an approver of a pending review in their own name,
    one without a fresh, unused re-authentication of the same session and
    method, and one whose hash is not the revision's; it spends the
    re-authentication. `review_decision_signed` refuses a decision in a
    signing project without a signature of the same reviewer and decision
    made in the same transaction. The app may record only a password
    re-authentication, for itself; the callback records the provider's.
  - **A re-authentication lasts at most ten minutes** and was given within
    them, or the row is refused.

## #47 Query language for requirements

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/requirements?query=` | view | `{rows, total, limit, offset}`; 400 `bad_query` with `position` |
| `GET /projects/{projectKey}/export.csv?query=`, `/coverage?query=`, `/matrix?query=`, `/matrix.csv?query=` | view | narrowed by the query; 400 `bad_query` |
| `POST /projects/{projectKey}/views`, `PATCH /views/{viewID}` with `filter.query` | as for views | `{view}`; 400 `bad_query` |
| `GET /projects/{projectKey}/requirements/suggest?q=&at=&limit=` | view | `{completion, words, requirements}`; 422 on `at` or `limit` |

- **The language** is Armature's: `field op value`, `IN (...)`, `NOT IN`,
  `IS [NOT] EMPTY`, `AND`, `OR`, `NOT`, brackets and a closing
  `ORDER BY field [DESC], ...`; operators `= != < <= > >= ~ !~`; values bare,
  quoted with `"` or `'`, numbers, days (`2026-10-15`, with `T10:30` for a
  time), durations (`-7d`, `2w`, `36h`) and functions (`currentUser()`,
  `now()`, `startOfDay()` to `endOfMonth()`, each taking a shift such as
  `startOfWeek(-1w)`). Keywords and field names ignore case. At most 2000
  characters, brackets and NOT 32 deep, 100 conditions.
- **The fields** every project has: `key`, `title`, `text` (the words box's
  search), `statement`, `rationale`, `kind`, `type`, `status`, `category`,
  `owner`, `author`, `priority` (`wont` to `must`, ordered), `labels`,
  `parent`, `revision`, `created`, `updated`, `suspect` (the count the grid
  shows), `approval` (`approved`, `pending`, `outdated`, `none`) and
  `decomposed`, `implemented`, `verified` (`none`, `partial`, `full`, `na`,
  as the coverage report gives them). An attribute is named in quotes,
  `"Risk" = High`, and compares as its kind does, options by name.
- **Links** give each link type a field per end, named after its phrase:
  `derivesFrom` and `derivedInto`, `verifies` and `verifiedBy`; a type that
  reads the same both ways, such as `relatesTo`, gives one field for either
  end. A phrase that would take a field's name falls back to the type's key,
  and `key` + `By` for the other end. Their values are requirement keys; an
  end in the trash links nothing.
- **A negation keeps what has nothing there**: `owner != currentUser()`
  keeps the requirements nobody owns, as in Armature.
- **Together**: the query is ANDed with `q` and the other parameters, and its
  `ORDER BY` outranks `sort`. A view keeps it in `filter.query`, refused when
  it does not run when saved; one that later names what the project no longer
  has is refused when it is read.
- **A refusal** is 400 `bad_query`, a sentence saying what to write instead,
  and `position`, the character it went wrong at, counted from 1.
- **Completion** answers where the caret `at` is, counted in characters (the
  end of `q` when absent): the slot (`field`, `operator`, `value`,
  `keyword`), the characters a pick replaces, the words the language takes
  there, the project's names for the field (statuses, types, labels, people,
  requirement keys), and the requirements the text finds: what it keeps when
  it is a query that runs, by key or title otherwise.
- UI: the grid's words box has a Words and Query switch; the query box
  completes at the caret, explains the fields in a dialog, and shows a
  refusal under the text with a caret at the character. The other controls
  stay and narrow with it.
- **As built** (#47):
  - **Compiled per project.** The catalog is the fixed fields, the project's
    attributes and its link types; a field per link type is its, and a typo
    is answered with the nearest name.
  - **Coverage in SQL.** `requirement_coverage` judges a level as
    `trace.Judge` does; an integration test holds every level to the report's
    before and after a revision.
  - **The database sees it as the reader.** `requirement_key_id` and
    `requirement_coverage` run as the caller, so a key or a level of a
    requirement the reader may not see is none, which a test proves through
    SQL as `exciter_app`.

## #48 Outbound webhooks

| Operation | Needs | Answers |
|---|---|---|
| `GET /webhooks` | an administrator | `{webhooks}`, without secrets |
| `POST /webhooks` | an administrator | `{webhook}` with its `secret`, this once; 422 |
| `PATCH /webhooks/{webhookID}` | an administrator | `{webhook}`; 422, 404 |
| `DELETE /webhooks/{webhookID}` | an administrator | 204; 404 |
| `POST /webhooks/{webhookID}/rotate-secret` | an administrator | `{webhook}` with its new `secret`, this once |
| `POST /webhooks/{webhookID}/test` | an administrator | `{delivery}`, the ping as logged |
| `GET /webhooks/{webhookID}/deliveries?limit=` | an administrator | `{deliveries}`, newest first; 422 on `limit` |
| `POST /webhooks/{webhookID}/deliveries/{deliveryID}/redeliver` | an administrator | `{delivery}` |

- **An endpoint** has a name, an address (http or https, no user name, no
  `#`), the topics it hears (`*` for every one, later ones too), the projects
  it listens to by key (none for every project, at most 100), and whether it
  is on. Its secret, `exciter_whs_` and 43 characters, is sealed with
  `EXCITER_SECRET_KEY`, bound to the installation and the endpoint, and
  shown in the answer that made or rotated it alone. Every route is the
  installation's administrators', `orgWide`, so a token held to some projects
  is refused.
- **The owner** is whoever saved the endpoint last; every payload is read
  with their rights when it is sent, and one whose owner was switched off
  sends nothing but a ping.
- **What is sent** is in [`docs/integration.md`](integration.md), and the
  `event` webhook of the API document.
- **The log** keeps one row per attempt for 30 days: the state (`pending`,
  `delivered`, `failed`, `withheld`, `cancelled`), the receiver's status and
  a sentence, never the body, the headers or what the receiver answered. A
  test ping and a redelivery are attempts by hand, neither retried nor
  counted against the endpoint.
- Audited: `webhook.created`, `webhook.updated`, `webhook.deleted`,
  `webhook.secret_rotated`, and `webhook.disabled` by nobody when the worker
  turns a failing endpoint off; the address is kept by its host alone.
- UI: Settings, Webhooks: a form with the events and the projects, the secret
  shown once with what signs a delivery, a list with each endpoint's state,
  events, projects and owner, and per endpoint a log, a test ping, on and off,
  rotating the secret and deleting.
- **As built** (#48):
  - **Queued by the worker, sent by its sender.** An event of a topic is
    queued for every endpoint on that hears it, and listens to its project:
    its requirement's, either end of a link's, or the one it names. The
    sender claims due deliveries every 2 seconds, leased so several workers
    never post one twice.
  - **The database holds the line.** Only an administrator acting with the
    whole installation reads or changes an endpoint or reads the log; the app
    never reads a sealed secret, names an owner, clears the counts or writes
    the log, and a topic nobody emits is refused.


## #133 Webhook envelope version 2

No new operations: the `version` of `POST /webhooks` and
`PATCH /webhooks/{webhookID}` takes 2 as well as 1, and the receivers at
`POST /armature/webhook` and `POST /stator/webhook` take a version 2 body.

- **Sent**: an endpoint on version 2 is posted `application/cloudevents+json;
  charset=utf-8` with `specversion` `1.0`, `id` (the event's), `source`
  (`EXCITER_APP_URL`), `type` (`dev.exciter.` and the topic), `time`,
  `datacontenttype` `application/json`, `dataversion` 1 and `data`, version
  1's payload. The headers and signatures are version 1's, the version
  header saying 2.
- **Chosen** when the endpoint is made, 1 when left out; saving without
  `version` keeps it; any other number is 422 on `version`, naming 1 or 2.
- **Received**: `type` after `dev.armature.` or `dev.stator.` is the topic,
  `data` the payload, `time` when it happened. 422 `unknown_version` for a
  version header other than 1 or 2, a `specversion` other than `1.0`, a
  `dataversion` other than 1 (absent is 1, a number or a string), or a header
  and a body naming different versions.
- `/meta` lists `webhooks@2` beside `webhooks@1`.
- UI: Settings, Webhooks: an Envelope choice in the form, each endpoint's
  version in the list, and a menu entry to switch it.
- **As built** (#133): one decoder in `internal/webhook` reads either
  version for both receivers; the stubs take `version` on
  `PUT /_stub/{tenant}/webhook` and post version 2 for every event after.

## #49 Requirement reuse and variants

| Operation | Needs | Answers |
|---|---|---|
| `PATCH /projects/{projectKey}` with `settings.library` | administer | `{project}`; audited as `project.library_set` |
| `POST /requirements/{reqKey}/reuse` | view of the library, edit of the target | 201 `{requirement, copied}`; 409 `not_a_library`; 422 on `projectKey` |
| `POST /requirements/{reqKey}/reuse/take` | edit, and view of the source | `{requirement}`; 403 without the source; 409 `source_moved_on`, `not_reused` |
| `POST /requirements/{reqKey}/reuse/variant` | edit | `{requirement}`; 409 `not_reused` |

- **A library** is a project its administrators mark as one. Only a
  library's requirements are reused, and only into another project, where a
  copy within the library would serve. Unmarking a library refuses new
  reuse and leaves what follows it alone.
- **A reuse** is a copy, made as copy makes one (#15), with what is below it
  when `children` is set, each copy following its own original. It records
  `reuse.source`, the source's `revision` it shows, and its `state`,
  `following` or `variant`. Links are not copied, and type, values, status,
  owner, priority, labels, links and reviews stay the target's.
- **While it follows**, a save, a restore or raw SQL that changes its title,
  statement or rationale is refused (409 `follows_source`). A take writes
  its next revision with the source's words at the source's latest revision,
  named as `revision` so a newer one is not taken unseen, and copies the
  files those words show; the comment says what was taken.
- **A variant** has words of its own, keeps the revision it branched from,
  and still reports `behind`; taking the source makes it follow again. Both
  turns are logged as the change field `reuse`.
- **Reading**: `reuse` on a requirement and a specification entry, with the
  source as the reader may read it, null to anybody else, and `behind` when
  the source has a newer revision the reader can see. The query field
  `reuse` is `following`, `behind`, `variant` or `none`.
- **Telling**: a new revision of a source tells the watchers of each live
  reuse who may read the source, as `source_revised`, about the reuse.
- **A purged source** leaves its reuses as requirements of their own.
- **The guard**: `requirement_reuse_guard` holds a reuse to a library
  requirement of another project, out of the trash, that the writer may
  read, at its revision now and with its words; refuses another source on a
  requirement; and holds a following copy's words and revision to the
  source's at its latest. Words are the title and the plain text of
  statement and rationale, so a copy's images, with ids of their own, still
  count as the source's.

## #43 Exciter requirements in Stator pages

No operations of Exciter's own: Stator builds the chips and blocks on what
Exciter already serves, checked by name at `/meta` (#140). This section is
the agreement with Stator, filed there for its side; #43 stays `blocked`
until Stator ships it.

| Capability | Operations | Stator uses it for |
|---|---|---|
| `requirement-embed@1` | `GET /requirements/{reqKey}`, `GET /search`, `GET /search/quick` | a chip and a requirement block, read by key; the picker |
| `requirement-list@1` | `GET /projects/{projectKey}/requirements`, `.../requirements/suggest` | a list block, a query in the query language; completing it in the block's editor |
| `specifications@1` | `GET /projects` | the projects the list block's editor offers |
| `webhooks@1` | `/webhooks` and its deliveries | knowing it may ask an Exciter administrator to point a webhook at Stator |

- **Connecting** is Stator's mirror of Exciter's #40: an administrator of
  Stator saves Exciter's origin, which answers `product` `exciter` at
  `GET /meta`, and each member stores their own Exciter personal access token
  (`exciter_pat_`, made under Your account, Access tokens; a read-only one
  serves). Every read is made with the viewer's token, so a page never shows
  a requirement its reader may not view in Exciter; a reader without a token
  sees the key as a link and how to connect. Exciter answers a requirement it
  will not show as 404, never 403.
- **A chip** stands for a key such as `CHK-R42` (`requirement.KeyPattern`,
  `^([A-Z][A-Z0-9]{1,9})-R([1-9][0-9]*)$`) written or picked on a page, and
  shows the title, the status and the revision, linking to
  `{exciter}/p/{projectKey}/r/{key}`. A key the requirement had before a move
  or a new project key still reads, and the answer's `key` is the current
  one, which Stator shows. A requirement in Exciter's trash carries
  `trashedAt` for a reader who may still find it there and is 404 to the
  rest; Stator shows it as trashed or as not found.
- **A requirement block** shows one requirement: the chip's fields with its
  statement and rationale, which are ProseMirror documents in Exciter's
  schema (headings, lists, tables, code, links, images by attachment URL);
  Stator renders what it knows and shows the rest as text.
- **A list block** names a project and a query, such as
  `status = Approved AND owner = currentUser() ORDER BY key`, and shows its
  rows as a table of key, title, status and revision, a window at a time
  (`limit` up to 500, `total` for paging). `currentUser()` is the viewer, so
  the same block reads differently to different readers. A query that does
  not read is `400 bad_query` with `position`, the character it went wrong
  at, which the editor shows under the text. The editor completes at the
  caret from `.../requirements/suggest` (`q`, and `at` for the caret).
- **The cache**, per viewer as Stator keeps Armature's issues: a chip or
  block is served from it for at most 60 seconds, and Exciter's webhooks end
  an entry sooner.
- **Webhooks.** An Exciter administrator adds a webhook (#48) for Stator's
  receiver, with the topics `requirement.created`, `requirement.revised`,
  `requirement.changed`, `requirement.trashed`, `requirement.restored`,
  `requirement.moved` and `review.decided`, and pastes the secret
  (`exciter_whs_`) into Stator's connection. Deliveries come in envelope
  version 1 with `source` `exciter`, signed in `X-Exciter-Signature-256`, and
  name the requirement by `id` and `key`. Stator uses them only to forget
  what it cached; the payload is read as the webhook's owner, so it never
  shows a reader what its own token would not.
- **Not in this contract:** writing to Exciter from a page, and an embed
  format such as oEmbed. Pages traced to requirements (#41, #42) stay as they
  are; a chip does not make a page link.

## #270 Suite demo

| Operation | Needs | Answers |
|---|---|---|
| `GET /installation/suite-demo` | an administrator | `{demo}`, null while nobody added it |
| `POST /installation/suite-demo` | an administrator | 202 `{demo}`; 409 when it was added; 422 `key` when a project holds `ARM`, `EXC` or `STA` |
| `POST /installation/suite-demo/links` | an administrator | 202 `{demo}`; 404 when nobody added it; 409 while it is built or linked |

- **The demo** is three projects from the `software` template, `ARM`, `EXC`
  and `STA`, holding the requirements of Armature, Exciter and Stator from
  the snapshot in `backend/internal/demo/snapshot` (`make demo-snapshot`).
  Each issue with a user story is a requirement filed under its milestone,
  under Functional requirements, with its acceptance criteria as children,
  approved when the issue is closed; "Depends on" is a `relates` link, also
  across projects; each project has an "About" requirement in its
  Introduction and a baseline named `Snapshot <taken>`. Audited as
  `installation.suite_demo_added`.
- **The run** is the worker's, as the caller: `suite_demo.requested` names a
  step, which makes a project, files one milestone, links one project or
  takes the baselines, and queues the next. `state` goes `queued`,
  `running`, then `done` or `failed`, with `step` of `steps`, and `message`
  saying why links wait.
- **Links** are made with the caller's own Armature and Stator tokens: each
  issue's requirement `implements` `<ARM|EXC|STA>-<number>`, and pages are
  found by the label `demo-<arm|exc|sta>-<path>` (`GET /labels/{labelName}/pages`
  in Stator); every requirement of milestone Mn `specifies` that tool's
  `docs/api-contract-mn.md`, and the About requirement is `describes`d by its
  README and `docs/architecture.md`. What a partner cannot take yet is
  counted in `skipped`; `.../links` adds it later and nothing twice.
- **Armature and Stator** offer `POST` and `GET /installation/suite-demo` for
  their part (Cloudster1/Armature#300, praetorianer777/Stator#457), which
  `make suite-up DEMO=1` calls before Exciter's; one that answers 404 has no
  demo yet.
