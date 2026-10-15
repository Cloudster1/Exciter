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

Package: `internal/reqifio`. Worker topic: `reqif.requested`. No settings.

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
  unpacked, an archive of at most 256 MB unpacked, and
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
  text shows as `files/<attachment id>.<ext>`, up to 256 MB; past either
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
