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

Packages: `internal/reqifio` (#44); `internal/specdoc`, `internal/docx` and `internal/render` (#45); `internal/review/signing.go`
and `internal/auth/reauth.go` (#46). Worker topic:
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
