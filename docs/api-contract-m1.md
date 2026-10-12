# API contract for M1: the requirements core

The agreement between whoever builds the backend and whoever builds the web
client of #11 to #24. As in Stator's contracts, the route table in
`backend/internal/httpapi/operations.go` is the source of truth for paths,
fields and types, and this file says what they mean. Every operation below is
in the table with `pending: true` and answers 501 `not_implemented` until it is
built; whoever builds one removes the mark, routes its handler and keeps this
file true. The types live in the domain packages named under each issue.

What is ported was read from Stator's `main` at cf0f6f7 and Armature's `main`
at 73d72dd. Where Exciter departs from them it says so, and
`docs/decisions.md` says why.

## Rules for every operation

- **Who.** All paths are under `/api/v1` and need a member of the
  organization. "View", "edit", "approve" and "administer" in the tables are the
  project rights of #12. "Admin" alone is an organization owner or
  administrator.
- **Existence is privileged.** A project or a requirement the caller may not
  view is 404, never 403. 403 is for a project the caller may view but not
  change in the way asked.
- **Keys in paths.** `projectKey` and `reqKey` are matched without regard to
  case. A key the project or requirement had before still resolves, for reads
  and writes alike, and the answer carries the current key. The web client
  replaces the address when the two differ.
- **Archived projects.** Reads work. Every write but unarchiving and deleting
  is 409 `project_archived`: "This project is archived. Unarchive it under its
  settings to change anything in it."
- **Errors** use the one envelope. A refused field is 422 `validation_failed`
  with `fields` naming it; nested fields are dotted, such as
  `attributes.<attributeId>` or `place.parentId`. The codes named here are the
  ones a client branches on, and every sentence says what to do.
- **Paging.** Lists that page take `limit` and `offset` and answer `total`,
  `limit` and `offset`, except the activity list, which takes a `cursor` and
  answers `next` like the audit log. A `limit` out of range is 422, never
  clamped.
- **Documents.** A statement, a rationale and a rich text attribute value are
  documents of the shared editor: ProseMirror JSON, validated against the
  allowlist of `internal/document` (#13), at most
  `requirement.MaxDocumentBytes` (1 MiB) each. The allowlist is Stator's cut
  to `doc`, `paragraph`, `heading` (levels 1 to 3), `bulletList`,
  `orderedList`, `listItem`, `blockquote`, `codeBlock`, `horizontalRule`,
  `table`, `tableRow`, `tableHeader`, `tableCell`, `text`, `hardBreak` and
  `mention`, with the marks `bold`, `italic`, `strike`, `code` and `link`.
  `api/document-allowlist.json` is the table as `make openapi` writes it,
  which the web editor's test holds its schema to.
  #21 adds `image` and `attachment`. The marks `diffInsert` and `diffDelete`
  exist only in comparisons (#16), and the allowlist refuses them in a save.
- **Normative content** is a requirement's title, statement, rationale, type
  and attribute values. Changing any of them writes the next revision. Owner,
  priority, status, labels, links, attachments and the place in the tree are
  logged in `requirement_change`, never revisioned (see `docs/decisions.md`,
  "A revision is a normative save").

## The data model

| Table | Holds |
|---|---|
| `project` | `key`, `name`, `description`, `next_number`, `settings`, `template`, `archived_at` |
| `project_key_alias` | a key a project had, which keeps resolving: `(org_id, key)` unique with `project.key` |
| `project_grant` | the role a user, a group or everyone holds in a project |
| `requirement` | `project_id`, `number`, `kind`, the normative content, `revision`, `normative_hash`, `status_id`, `owner_id`, `priority`, `parent_id`, `rank`, the trash columns, `copied_from` |
| `requirement_old_key` | a key a requirement left on a move to another project |
| `requirement_revision` | `number`, the normative content as it was, `hash`, `comment`, `restored_from`, author; append only |
| `requirement_change` | one logged change: `field`, `from_value`, `to_value`, `reason`, actor; append only |
| `requirement_type` | a project's kinds of requirement, and the workflow of each |
| `attribute_def`, `attribute_def_type` | a project's attributes, and the types each shows on |
| `req_status`, `workflow`, `workflow_step`, `workflow_transition`, `transition_rule` | a project's statuses and workflows |
| `link_type`, `link_type_project`, `link_type_rule` | the organization's and the projects' link types, whether a project uses one, and which requirement types it may join |
| `trace_link` | `type_id`, `source_id`, `target_id`, `source_reviewed_rev`, `target_reviewed_rev`, `deleted_at` |
| `requirement_label` | a label on a requirement |
| `saved_view` | a person's view of a project's grid |
| `attachment` | a file on a requirement |
| `api_token`, `api_token_project` | personal access tokens, and the projects one is limited to |
| `import_job` | a CSV file on its way into a project |

Every table has `org_id` and the composite keys and row level security of
`docs/architecture.md`. Every table that belongs to a requirement carries the
restrictive policies of #12.

## Migrations, packages and settings

| Range | Owner | Holds |
|---|---|---|
| 00100 to 00109 | projects (#11) | `project`, `project_key_alias`, their policies |
| 00110 to 00119 | permissions (#12) | `project_grant`, `perm_project_holds` and its helpers |
| 00120 to 00139 | requirements (#13) | `requirement`, `requirement_old_key`, `requirement_revision` with its sequence trigger, `requirement_change`, the trash, `perm_requirement_viewable` and `perm_requirement_editable` |
| 00140 to 00149 | types and attributes (#14) | `requirement_type`, `attribute_def`, `attribute_def_type`, `requirement.type_id`, the same-project guard on `requirement.attributes` and its GIN index |
| 00150 to 00159 | hierarchy (#15) | `requirement.parent_id` and `rank`, the cycle guard |
| 00160 to 00169 | history (#16) | nothing is planned; kept for what building it finds |
| 00170 to 00179 | workflow (#17) | `req_status`, `workflow`, `workflow_step`, `workflow_transition`, `transition_rule`, `requirement.status_id` and the functions that alone change it |
| 00180 to 00189 | trace links (#18) | `link_type`, `link_type_project`, `link_type_rule`, `trace_link`, the cycle guard, the seeded types |
| 00190 to 00199 | labels (#19) | `requirement_label` |
| 00200 to 00219 | search (#20) | the `exciter_search` configuration, `requirement_plain_text`, the search vector, `saved_view` |
| 00220 to 00229 | attachments (#21) | `attachment`, `attachment_tombstone` |
| 00230 to 00239 | tokens (#22) | `api_token`, `api_token_project` |
| 00240 to 00249 | import (#23) | `import_job` |
| 00250 to 00259 | templates (#24) | nothing is planned: built-ins live in code and `project.template` records the choice |

A range adds its columns to tables of an earlier range with `ALTER TABLE`, so
`requirement` gains `type_id` in 00140 and `status_id` in 00170.

Packages: `internal/project`, `internal/perm`, `internal/requirement`
(requirements, revisions, the tree and the trash), `internal/attribute`
(types and attributes), `internal/workflow`, `internal/trace`,
`internal/label`, `internal/search`, `internal/attachment`, `internal/csvio`,
`internal/document` and `internal/rank` (ported from Stator), and the token
types in `internal/auth`.

New setting, in compose and in `internal/config`:

| Setting | Meaning |
|---|---|
| `EXCITER_UPLOAD_LIMIT` | The largest attachment in bytes; a suffix such as `50MB` works. 50 MB by default (`attachment.DefaultMaxSize`). |

### Events and the audit log

New outbox topics, which M2 and M3 build on. Every payload carries `actorId`
and the ids it names.

| Topic | When | Payload |
|---|---|---|
| `requirement.created` | create, copy, import | `projectId`, `requirementId`, `revision` |
| `requirement.revised` | a save or a restore that wrote a revision | `requirementId`, `revision` |
| `requirement.changed` | a logged change | `requirementId`, `field` |
| `requirement.trashed`, `requirement.restored` | the trash | `requirementId` |
| `requirement.moved` | a move, with the old and new key | `requirementId`, `fromKey`, `toKey` |
| `project.key_changed` | a new project key | `projectId`, `fromKey`, `toKey` |
| `link.created`, `link.removed` | trace links | `linkId`, `sourceId`, `targetId` |
| `import.requested` | an import started | `importId` |
| `project.deleted` | a project deleted, so the worker removes its files | `projectId` |

New audit actions: `project.created`, `project.key_changed`,
`project.archived`, `project.unarchived`, `project.deleted`,
`project.grants_changed`, `requirement.purged`, `trash.emptied`,
`workflow.saved`, `workflow.assigned`, `link_type.created`,
`link_type.changed`, `link_type.deleted`, `token.created`, `token.revoked` and
`import.started`. Requirement edits are not audited: their history holds them.

## #11 Projects

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects?archived` | member | `{projects}`, by name |
| `POST /projects` | admin | 201 `{project}`; 422 on `key`, `name`, `template` |
| `GET /projects/key-check?key&name` | member | `{check}` |
| `GET /projects/{projectKey}` | view | `{project}` |
| `PATCH /projects/{projectKey}` | administer | `{project}` |
| `PUT /projects/{projectKey}/key` | administer | `{project}`; 422 on `key` |
| `PUT /projects/{projectKey}/archive` | administer | `{project}` |
| `DELETE /projects/{projectKey}/archive` | administer | `{project}` |
| `DELETE /projects/{projectKey}` | administer | 204; 409 `project_not_archived` |

- **Keys** match `^[A-Z][A-Z0-9]{1,9}$`, as Armature's and Stator's do. What
  is typed is trimmed and upper-cased first (`NormalizeKey`). A key is taken
  when a project has it now or had it before (`project_key_alias`), so an old
  key never points at two projects; the database holds both in one unique
  index. Sentences: "A project needs a key: two to ten letters or digits, such
  as CHK." and "A key is two to ten capital letters or digits and starts with a
  letter, such as CHK or SYS2." and "The key CHK is taken by another project.
  Choose another."
- **Key check** answers for `key` whether it is `valid` and `available`, and
  for `name` a free `suggestion`: `SuggestKey` of the name (initials of
  several words, the leading letters of one), with 2, 3 and so on appended,
  shortened to fit ten characters, until it is free.
- **Create** is for owners and administrators of the organization, and is
  refused a token limited to projects. It makes the project from the template
  named (#24), or blank: one type `Requirement` that is the default type, the
  default statuses and workflow (#17), every organization link type enabled,
  and no sections. The creator is granted `admin` and everyone `editor` (#12).
  Each of #17, #18 and #24 adds its part to create as it lands: until then a
  blank project is the row, its grants and its type `Requirement`, which a
  trigger makes and names `settings.defaultTypeId`, and a `template` is 422.
- **A new key** (`PUT .../key`) keeps the old one as an alias: `CHK-R5`
  becomes `SYS-R5`, and both `CHK` and `CHK-R5` keep resolving. Taking back a
  key the project had before removes it from the aliases. The same key is no
  change. It emits `project.key_changed` and is audited.
- **Settings**: `defaultTypeId` (a type of the project, not retired),
  `requireRevisionComment` (a save that writes a revision without a comment is
  422 on `comment`), and `workflowId`, which is read here and set with
  `PUT /projects/{projectKey}/workflow` (#17).
- **Archive** as Stator archives a space: the project stays readable, leaves
  the project list unless `archived=true`, search and the pickers, and nothing
  in it changes. Archiving twice is no change.
- **Delete** takes an archived project, else 409 `project_not_archived`:
  "Archive the project first. Deleting it removes every requirement and its
  history for good." Everything in it goes in one transaction. Its files go
  by tombstones, which the worker clears after `project.deleted`. Links from
  other projects to it go too.
- `Project.can` is the caller's rights in it (#12). `requirements` counts
  requirements and sections out of the trash. `formerKeys` are its aliases,
  the latest first.
- UI: `/projects` lists and creates. `/p/$projectKey/settings` holds details,
  key, archive and delete, with types, attributes, workflow and link types as
  tabs that #14, #17 and #18 fill. The grants have their own page (#12). The
  sidebar shows the current project's pages under a project switcher.

## #12 Permissions

| Operation | Needs | Answers |
|---|---|---|
| `GET /access/me` | member | `{can: {createProject, administer}}` |
| `GET /people?q&limit`, `GET /groups?q&limit` | member | pickers, 20 by default, at most 50 |
| `GET /projects/{projectKey}/grants` | administer | `{grants}` |
| `PUT /projects/{projectKey}/grants` | administer | `{grants}`; 409 `no_administrator`; 422 |
| `GET /projects/{projectKey}/access/{userID}` | administer | `{access}`; 404 when the person is not a member |

- **Roles and rights.** A grant gives a user, a group or `everyone` one role
  in a project:

  | Role | view | comment | edit | approve | administer |
  |---|---|---|---|---|---|
  | `viewer` | yes | | | | |
  | `reviewer` | yes | yes | | yes | |
  | `editor` | yes | yes | yes | | |
  | `admin` | yes | yes | yes | yes | yes |

  What a person holds is the union of every grant that reaches them: their
  own, their groups' and everyone's. So a person who is `editor` through a
  group and `reviewer` on their own both edits and approves.
- **What a right allows.** `view` is reading the project, its requirements,
  history, links, labels and files, and exporting. `comment` waits for M3.
  `edit` is creating, changing, moving, copying into, trashing and restoring
  requirements, linking them, labelling them, attaching files and taking
  transitions. `approve` is taking the transitions that only reviewers may
  take (#17), and the approvals of #35. `administer` is the project's
  settings, key, archive, grants, types, attributes, statuses, workflows, link
  types, purging and emptying the trash, imports and deletion.
- **Everyone** is every member of the organization, guests left out. A guest
  is reached only by grants that name them or one of their groups, and holds
  at most `editor`. A grant of `admin` to `everyone` or a guest is 422, and
  the database refuses both. A group's `admin` grant counts as `editor` for a
  guest in that group.
- **Organization owners and administrators** hold `admin` in every project,
  so no project is orphaned. A token limited to other projects does not.
- **Defaults.** A new project grants its creator `admin` and everyone
  `editor`, which a template may narrow (#24). A trigger on `project` makes
  these grants, so a project made any other way gets them too.
- **PUT** replaces the whole table. A row without a role, a role that is not
  one of the four, an unknown subject, or a subject named twice is 422 on
  `grants.<n>.role` or `grants.<n>.subject`. A table with no `admin` row
  naming a user or a group is 409 `no_administrator`: "Give at least one
  person or group the admin role, so somebody besides the organization's
  administrators can manage the project." The change is audited.
- **Pickers** as Stator's: they match the start of a person's name, of any
  word of it, or of their email, and the start of a group's name or of any
  word of it, ignoring case. `limit` outside 1 to 50 is 422. The owner picker
  of a requirement uses `/people`.
- **The access inspector** answers for one member and one project each of
  the five rights, with `allowed` and `steps`. An organization owner or
  administrator gets the one step `orgAdmin`. Anybody else gets `guest`, then
  `grant`. `guest` is passed unless the person is a guest whom only
  everyone's grant would have given the right. `grant` lists the project's
  `grants` that reach the person and give the right. An archived project adds
  a failed `archived` step to comment, edit and approve. The first step not
  passed is the reason for a no. Every verdict is what `perm_project_holds`
  answers for that person, so the explanation is what the database enforces.
- **Reading the grants** needs `administer`, in the database too, and
  changing them also needs the project not to be archived.
- **In the database.** `perm_project_holds(project uuid, actor uuid,
  permission text)`, `perm_requirement_viewable(requirement uuid, actor uuid)`
  and `perm_requirement_editable(requirement uuid, actor uuid)` are `STABLE
  SECURITY DEFINER` SQL functions, as Stator's `perm_page_viewable` and its
  kin are. Every table that belongs to a requirement uses them as
  `RESTRICTIVE` policies: reading needs viewable, writing needs editable. The
  integration suite proves each guard by trying the write as `exciter_app`
  straight through SQL. `perm.ViewableRequirement(alias, n)` is the Go
  condition every query that lists requirements narrows itself with, so
  search, the grid, the tree and labels count only what the caller sees.
- **`project.can`** is `view`, `comment`, `edit`, `approve`, `administer` and
  `purge` (administer, while the project is not archived); `requirement.can`
  is `edit`, `transition` (edit or approve), `delete`, `link` and `comment`.
  An archived project keeps `view` and `administer`, which is what unarchives
  and deletes it; every other change to it is 409 `project_archived`. The web
  client offers only what `can` allows.
- UI: `/p/$projectKey/permissions`, linked from the project's sidebar for
  its administrators: a table of subjects with a role each, a picker of
  people, groups and everyone, and the access inspector in a dialog.

## #13 Requirements

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/requirements` | edit | 201 `{requirement}`; 422 |
| `GET /requirements/{reqKey}` | view | `{requirement}` |
| `GET /requirements/by-id/{requirementID}` | view | `{requirement}` |
| `PATCH /requirements/{reqKey}` | edit | `{requirement}`; 409 `revision_conflict`; 422 |
| `DELETE /requirements/{reqKey}` | edit | 204 |
| `GET /projects/{projectKey}/trash` | edit | `{items}`, the latest first |
| `POST /projects/{projectKey}/trash/{requirementID}/restore` | edit | `{requirement}` |
| `DELETE /projects/{projectKey}/trash/{requirementID}` | administer | 204 |
| `DELETE /projects/{projectKey}/trash` | administer | 204 |

- **Keys.** `<PROJECT>-R<n>`, `^([A-Z][A-Z0-9]{1,9})-R([1-9][0-9]*)$`. The
  number comes from `project.next_number`, taken under the project row's lock
  in the creating transaction, so numbers have no gaps. A number is never
  reused, even after a purge. Sections take numbers like requirements, so
  every node has a permalink. A unit test feeds Exciter keys through copies of
  Armature's and Stator's issue-key patterns and none matches.
- **Kinds.** `requirement` (the default) or `section`. A section has a title
  and an optional statement, which is its introduction, and no rationale,
  type, attributes, status, owner, priority or links: giving one is 422 on
  that field. Sections are revisioned like requirements.
- **Create.** `title` is trimmed, 1 to `requirement.MaxTitleLength` (300)
  characters. `statement` may be an empty document. The type is `typeId`, or
  the project's default type. Attribute values are checked as #14 says. The
  status is the initial status of the type's workflow. `ownerId` names a
  member who may view the project. `place` is where it goes (#15), and it goes
  last at the top when left out. `comment`, at most
  `requirement.MaxCommentLength` (500) characters, goes with revision 1.
- **Saving.** `PATCH` changes what it names and leaves the rest alone. When
  the normative content it sends differs from the current, it writes revision
  `requirement.revision + 1` and must carry `baseRevision`. Without it the
  answer is 422 on `baseRevision`. With one that is not the current revision
  it is 409 `revision_conflict`: "Somebody saved a newer revision of CHK-R42
  while you were editing. Compare it with your changes, then save again over
  it." As in Stator, the refusal holds the caller's next reads to the state it
  was decided on, so the comparison finds the newer revision. Owner and
  priority need no `baseRevision`. They are logged, and the last save wins.
  The status moves only by transitions (#17), labels by #19, and the place by
  #15.
- **The normative hash** is the SHA-256 of the canonical JSON of title,
  statement, rationale, type id and attribute values: object keys sorted,
  runs of white space in text collapsed and trimmed, empty paragraphs at the
  ends dropped. A save whose hash equals the current one writes no revision,
  so a revision always says something new. A unit test holds the hash stable
  under key order and white space.
- **Revisions are immutable.** `exciter_app` may insert into
  `requirement_revision` and read it, never update or delete it. A trigger in
  the manner of Stator's `page_version_next` refuses a number that is not the
  next one, under the requirement row's lock. The integration suite proves
  both through SQL.
- **Priority** is `must`, `should`, `could` or `wont`, or null. It is not
  normative. A project that wants a normative priority defines an attribute.
- **The trash** works as Stator's: `DELETE` marks the requirement and
  everything below it with one `trash_id`, and they keep their parent and
  rank. A trashed requirement is 404 to readers. An editor gets it with
  `trashedAt` set, so a permalink can offer the restore. Its links are left
  out of every list and of coverage until it comes back. Restore puts it back
  under its parent, or at the top when the parent is gone. Purge deletes it,
  what went with it, their revisions, changes, links and files for good, and
  is audited.
- **Permalinks.** The web client's `/r/$requirementId` reads
  `GET /requirements/by-id/{requirementID}` and replaces itself with
  `/p/$projectKey/r/$reqKey`. A key a requirement left on a move (#15), or a
  key under a project's former key (#11), resolves to the requirement:
  `requirement_old_key` first, then the project's aliases with the same
  number.
- **The editor** is a slimmed port of Stator's `Editor`, `Toolbar`, `DocView`
  and `MentionList`, with the allowlist above. A vitest test holds the
  client's node and mark list equal to the server's, attributes included,
  which is why table cells take Tiptap's `align` (left, center, right).
- **Numbers** are handed out by a trigger on `requirement`, whatever an insert
  says, so even a write straight through SQL takes the next one.
- **A trashed requirement** changes only by its restore: a `PATCH` is 409
  `requirement_trashed`, "This requirement is in the trash. Restore it from
  the project's trash to change it."
- **`requireRevisionComment`** asks for a comment on every revision after the
  first; revision 1 is the requirement's writing.
- **Until the later issues land**, `labels` is 422 (#19) and `status` is
  null (#17). A requirement written without a place goes last at the top. `GET
  /projects/{projectKey}/requirements` is built here, in key order and paged,
  narrowed by `kind`, `owner` and `priority`; its other parameters are 422
  until #20 builds them. Its tool is `list_requirements`.
- UI: `/p/$projectKey` lists the requirements and is where a new project
  opens, `/p/$projectKey/new` writes one, and `/p/$projectKey/trash` holds the
  trash. `/p/$projectKey/r/$reqKey` shows the statement, rationale, attributes,
  status, owner, priority, labels, links and files, edited in place.

## #14 Types and attributes

| Operation | Needs | Answers |
|---|---|---|
| `GET /attribute-kinds` | member | `{kinds}` |
| `GET /projects/{projectKey}/types` | view | `{types}`, in order, retired included |
| `POST /projects/{projectKey}/types` | administer | 201 `{type}`; 422 |
| `PATCH /projects/{projectKey}/types/{typeID}` | administer | `{type}` |
| `DELETE /projects/{projectKey}/types/{typeID}` | administer | 204; 409 `in_use` |
| `GET /projects/{projectKey}/attributes` | view | `{attributes}`, in order, retired included |
| `POST /projects/{projectKey}/attributes` | administer | 201 `{attribute}`; 422 |
| `PATCH /projects/{projectKey}/attributes/{attributeID}` | administer | `{attribute}`; 422 |
| `DELETE /projects/{projectKey}/attributes/{attributeID}` | administer | 204; 409 `in_use` |

- **Types** belong to a project. A type has a name, unique in the project
  without regard to case, at most `attribute.MaxNameLength` (60) characters.
  It also has a description, an `icon` naming one of the web client's type
  icons (`TYPE_ICONS` in `web/src/config.ts`; the server checks the shape
  `^[a-z][a-z0-9-]{0,39}$`), a `colour` as `#rrggbb`, a position, and its own
  workflow or none (#17). Changing a requirement's type is normative. A type
  whose workflow lacks the requirement's status puts it in that workflow's
  initial status, logged with the reason.
- **Retire, rather than delete.** A type, an attribute or an option that was
  ever used is retired: it keeps its place on the requirements and revisions
  that have it, leaves the choosers, and cannot be chosen again. `DELETE`
  removes only what no requirement and no revision ever had, and is otherwise
  409 `in_use`: "This type is used by 12 requirements. Retire it instead, so
  their history keeps it." The project's default type cannot be retired (422
  on `retired`), and deleting it is 409 `default_type`: "This is the
  project's default type. Make another type the default under the project's
  settings first." Whether something was used counts every requirement,
  trashed or not, and every revision, whoever asks.
- **Projects made before types** got the type `Requirement` as their
  default; their requirements keep no type, so what their revisions say stays
  as it was.
- **Attribute kinds**, and what a value is:

  | Kind | Value |
  |---|---|
  | `text` | a string, trimmed, at most `attribute.MaxTextLength` (500) characters |
  | `richText` | a document of the shared editor |
  | `number` | a JSON number; NaN and infinity are refused |
  | `select` | the id of one option |
  | `multiSelect` | an array of option ids, at most `attribute.MaxMultiSelect` (50), kept in the attribute's order |
  | `date` | `YYYY-MM-DD` |
  | `user` | the id of a member who may view the project |
  | `boolean` | `true` or `false` |
  | `url` | an absolute `http` or `https` address with a host |

  `null`, an empty string and an empty array clear a value. A value of the
  wrong shape is 422 on `attributes.<attributeId>` with a sentence that says
  what fits.
- **Where values live.** `requirement.attributes` is a JSON object keyed by
  attribute id, GIN-indexed. A trigger refuses a key that is not an attribute
  of the requirement's own project, which the integration suite proves through
  SQL. Every revision copies the values it had.
- **Which types.** `typeIds` names the types an attribute shows on, and empty
  means every type. A value for an attribute that does not show on the
  requirement's type is 422. Values a requirement keeps after its type changes
  stay in its revisions and leave the form.
- **Required** attributes need a value when a requirement of a type they show
  on is created, and on every save that writes a revision. Making an
  attribute required later asks for its value at the next save, not before.
- **Options** have ids, which values name, so renaming an option renames it
  everywhere, history included. `PATCH` with `options` replaces the list:
  sending one without an id adds it, and leaving one out retires it, as does
  `retired: true`. A retired option stays on the values that chose it, and a
  save may keep it there unchanged, but no value may newly choose it. Options
  are trimmed, 1 to `attribute.MaxOptions` (100), unique in the attribute
  without regard to case.
- **The kind** is set when an attribute is made and never changes.
- UI: the Types and Attributes tabs of the project settings, ported from
  Armature's `features/fields`; the default type is chosen under Details. A
  requirement's form offers its type and the attributes that show on it, and
  its page shows the values given, options and people by name.

## #15 Hierarchy, sections and the specification view

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/tree` | view | `{nodes}`, in reading order |
| `GET /projects/{projectKey}/spec?limit&offset` | view | `{entries, total, limit, offset}` |
| `POST /requirements/{reqKey}/move` | edit, in both projects | `{requirement}`; 422 |
| `POST /requirements/{reqKey}/copy` | view, and edit where it goes | 201 `{requirement, copied}`; 422 |

- **The tree.** Every requirement and section has a parent (another one of
  the same project, or null for the top) and a `rank` among its siblings,
  Stator's `internal/rank`. Anything may sit below anything. A trigger in the
  manner of Stator's `00021_page_tree.sql` refuses a parent that is the node
  itself or below it: 422 on `place.parentId`, "A requirement cannot go below
  itself or below one of the requirements under it." The integration suite
  proves it through SQL. Moves within a project take the project's tree lock,
  so two concurrent moves cannot make a cycle between them.
- **Outline numbers** are computed on read and never stored: the 1-based
  position among the siblings out of the trash, joined by dots, sections and
  requirements alike. The tree and the specification answer them, and
  `web/src/features/tree/outline.ts` numbers a drag preview by the same rule,
  which a vitest test holds.
- **`GET .../tree`** is the whole tree out of the trash in one answer: the
  sidebar and the specification view both need all of it. It carries no
  content. **`GET .../spec`** is a window of the same order with content, 20
  by default and at most `requirement.MaxSpecLimit` (200), which the
  specification view reads as the reader scrolls.
- **Place** is `{parentId, afterId}`: `afterId` names the sibling it follows,
  and null puts it first. An `afterId` that is not a child of `parentId` is
  422.
- **Move** puts the requirement and everything below it at `place`. In the
  same project it changes nothing but the place, logged as `place`. To
  another project (`projectKey`), each moved requirement gets a key from the
  target's counter, in reading order, and keeps its old key in
  `requirement_old_key`. Its type and attributes are matched by name in the
  target, attributes also by kind and options by name. A value without a match
  is dropped, a type without one becomes the target's default, and when that
  changes the normative content the move writes a revision with the comment
  "Moved from CHK-R5". Statuses are matched by name, and `statusMapping` maps
  each one the target's workflow lacks; one left unmapped is 422 on
  `statusMapping` naming it. Links, labels and files go along. It emits
  `requirement.moved` for each.
- **Copy** makes new requirements at the current content as revision 1, with
  `copiedFrom` and the comment "Copied from CHK-R5". `children` copies
  everything below. `links` copies the links among the copied requirements
  between their copies, and those to the rest from the copy's end. Labels and
  files are copied. Statuses start at the initial one. Into another project,
  types and attributes match as for a move. At most `requirement.MaxCopy`
  (1000) requirements are copied, else 422. `copied` counts them.
- **In the database.** A trigger refuses a parent of another project or below
  the node itself, under the project's tree lock (both projects' for a move
  between two, in one order), so even moves made straight through SQL cannot
  close a loop. A requirement changes project only by a move: a trigger gives
  it the next number there and writes its old key, whoever moves it. `rank`
  compares in byte order (`COLLATE "C"`), whatever the database's collation.
- **The trash** takes everything below a requirement as one item. A restore
  whose parent went to the trash on its own, or for good, puts the item at the
  top.
- **Until the later issues land**, a move between projects refuses a
  `statusMapping` (422 until #17), and `links` on a copy waits for #18. Links,
  labels and files belong to the requirement's id, so a move takes them along
  as they arrive.
- UI: `/p/$projectKey/spec` shows the tree beside the numbered document,
  edited in place. Dragging in the tree moves, and the place dialog moves or
  copies to another project, ported from Stator's `PageTree` and
  `PlaceDialog`; the dialog places last below the parent chosen. A
  requirement's page offers Move and Copy, and shows where a copy came from.

## #16 History, compare and restore

| Operation | Needs | Answers |
|---|---|---|
| `GET /requirements/{reqKey}/revisions?limit&offset` | view | `{revisions, total, limit, offset}`, the latest first |
| `GET /requirements/{reqKey}/revisions/{revisionNumber}` | view | `{revision}` with content |
| `GET /requirements/{reqKey}/diff?from&to` | view | `{comparison}` |
| `POST /requirements/{reqKey}/revisions/{revisionNumber}/restore` | edit | `{requirement, revision}`; 409 |
| `GET /requirements/{reqKey}/activity?limit&cursor` | view | `{entries, next}`, newest first |

- **History** lists each revision's number, title, comment, author, time,
  `restoredFrom` and hash. `limit` is 1 to 100, 20 by default.
- **Compare** any two revisions of one requirement, in either order. `to`
  defaults to the latest revision. `from` defaults to the one before `to`,
  and `from=0` is the empty requirement. Statement and rationale compare as
  Stator compares page bodies (`page/diff.go`): `blocks` in reading order,
  each `{change, node}`, a modified block carrying `diffInsert` and
  `diffDelete` marks inline. `attributes` lists each attribute that differs,
  or is set on either side, with `change`, both raw values, and `fromText` and
  `toText` as a reader sees them: options and people by their names now, and
  dates as written. The type shows on each side.
- **Restore** writes revision N's content as revision `revision + 1`, with
  `restoredFrom: N` and the comment given, or "Restored revision N".
  `baseRevision` must be the current revision, else 409 `revision_conflict`.
  Restoring the latest revision, or one whose content equals the current, is
  409 `no_change`: "That revision says what the requirement says now. Pick
  another to restore." Retired types, attributes and options come back as
  they were. A required attribute the revision lacks is asked for at the next
  save, not refused.
- **Activity** merges revisions and logged changes, newest first, paged by
  cursor like the audit log: a revision entry carries the revision, and a
  change entry its field, what it was and became as a reader sees them, and
  why when it was not the actor's own choice (such as a revision taking an
  approved requirement back to Draft).
- UI: `/p/$projectKey/r/$reqKey/history`, ported from Stator's `PageHistory`
  and `page-history.tsx`.

## #17 Statuses and workflow

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/statuses` | view | `{statuses}` |
| `POST /projects/{projectKey}/statuses` | administer | 201 `{status}`; 422 |
| `PATCH /projects/{projectKey}/statuses/{statusID}` | administer | `{status}` |
| `DELETE /projects/{projectKey}/statuses/{statusID}` | administer | 204; 409 `in_use` |
| `GET /projects/{projectKey}/workflows` | view | `{workflows}` |
| `POST /projects/{projectKey}/workflows` | administer | 201 `{workflow}`; 422 |
| `GET /projects/{projectKey}/workflows/{workflowID}` | view | `{workflow}` |
| `PUT /projects/{projectKey}/workflows/{workflowID}` | administer | `{workflow}`; 409 `status_in_use`; 422 |
| `DELETE /projects/{projectKey}/workflows/{workflowID}` | administer | 204; 409 `in_use` |
| `PUT /projects/{projectKey}/workflow` | administer | `{workflow}`; 422 on `statusMapping` |
| `GET /workflow-rule-types` | member | `{ruleTypes}` |
| `GET /requirements/{reqKey}/transitions` | view | `{transitions}` |
| `POST /requirements/{reqKey}/transitions` | edit or approve | `{requirement}`; 409 `transition_unavailable`; 422 |

- **A slimmed Armature workflow**, without schemes: statuses and workflows
  belong to a project. The project follows one workflow, and a type may follow
  its own instead (`requirement_type.workflow_id`).
- **Categories**: `draft`, `review`, `approved`, `done` and `closed`.
  Coverage (#38) leaves `closed` out. Filters and reports key on the
  category, never on a status's name.
- **The default workflow**, which every new project gets: Draft (draft), In
  review (review), Approved (approved), Implemented (done), Verified (done),
  Rejected (closed), Obsolete (closed). Its transitions:

  | Transition | From | To | Rules |
  |---|---|---|---|
  | Submit for review | Draft | In review | role editor, admin |
  | Approve | In review | Approved | role reviewer, admin; not the author |
  | Send back | In review | Draft | role reviewer, admin; comment required |
  | Mark implemented | Approved | Implemented | role editor, admin |
  | Mark verified | Implemented | Verified | role editor, admin |
  | Reject | any | Rejected | role reviewer, admin; comment required |
  | Make obsolete | any | Obsolete | role editor, admin |
  | Reopen | Rejected | Draft | role editor, admin |
  | Reopen | Obsolete | Draft | role editor, admin |

  Approved, Implemented and Verified go back to Draft on a new revision
  (`onRevisionStatusId`).
- **Back on a new revision.** A step may name where a requirement in it goes
  when a revision is saved. The save moves it there in the same transaction,
  logged as a status change with the reason "Revision 4 changed an approved
  requirement." This is how a new revision supersedes an approval.
- **Rule types** (`GET /workflow-rule-types`), Armature's registry made for
  requirements:

  | Type | Kind | Config |
  |---|---|---|
  | `condition.project_role` | condition | `{"roles": [...]}`, of viewer, reviewer, editor, admin, required |
  | `condition.is_owner` | condition | none |
  | `condition.not_author` | condition | none: the caller did not write the latest revision |
  | `validator.comment_required` | validator | none |
  | `validator.owner_required` | validator | none |
  | `validator.required_attributes` | validator | none: every required attribute has a value |
  | `postfunction.assign_to_actor` | postfunction | none |
  | `postfunction.clear_owner` | postfunction | none |

  A failing condition hides the transition. A failing validator answers 422
  with its sentence on `comment`, `ownerId` or `attributes.<attributeId>`.
- **Who may take a transition.** It needs `edit`, or `approve` when its
  conditions include `condition.project_role` naming `reviewer`, which is how
  a reviewer approves without editing. Its conditions decide the rest.
- **Status changes only by transition.** `exciter_app` has no `UPDATE` on
  `requirement.status_id`. The column changes only through `SECURITY DEFINER`
  functions: `requirement_transition`, which checks the transition leaves the
  current status (or is global), lands where it says and is the caller's to
  take, and `requirement_remap_status`, used by moves and by assigning a
  workflow, which takes only a status the new workflow lacks to one it has.
  The integration suite proves the column refuses a plain `UPDATE`.
- **Taking a transition** whose start is no longer the requirement's status
  is 409 `transition_unavailable`: "Somebody moved CHK-R42 to Approved
  meanwhile. Pick a transition from there." The change is logged, never a
  revision, and emits `requirement.changed`.
- **The designer** saves a whole workflow in one request, as Armature's does:
  a transition sent with an id it has keeps that id. Dropping a status that
  requirements under this workflow are in is 409 `status_in_use` naming how
  many; move them first, or assign another workflow with a mapping. The graph
  is checked as Armature's `ValidateGraph` checks it: a name, at least one
  status, statuses of the project and each once, exactly one initial status,
  every transition named and inside the workflow, no transition to its own
  start, names unique per start, global counting as one start. Limits:
  `workflow.MaxStatuses` (50), `MaxTransitions` (200) and `MaxRulesPerEdge`
  (20).
- **Assigning** (`PUT /projects/{projectKey}/workflow`) makes a workflow the
  project's (`typeId` left out) or a type's. Every requirement it affects
  whose status the workflow lacks needs `statusMapping[statusId]`, and a
  missing one is 422 naming the statuses. It is audited.
- **Statuses** are named uniquely in the project without regard to case. A
  status that a workflow or a requirement uses cannot be deleted (409
  `in_use`).
- UI: the Workflow tab of the project settings, with the graph designer
  ported from Armature's `features/workflows`, and the transition buttons on
  a requirement.

## #18 Typed trace links

| Operation | Needs | Answers |
|---|---|---|
| `GET /link-types` | member | `{linkTypes}`, the organization's |
| `POST /link-types` | admin | 201 `{linkType}`; 422 |
| `PATCH /link-types/{linkTypeID}` | admin, or administer for a project's own | `{linkType}`; 422 |
| `DELETE /link-types/{linkTypeID}` | as PATCH | 204; 409 `in_use` |
| `GET /projects/{projectKey}/link-types` | view | `{linkTypes}`, with `enabled` and `rules` |
| `POST /projects/{projectKey}/link-types` | administer | 201 `{linkType}`; 422 |
| `PUT /projects/{projectKey}/link-types/{linkTypeID}` | administer | `{linkType}`; 422 |
| `GET /requirements/{reqKey}/links` | view | `{links}` |
| `POST /requirements/{reqKey}/links` | edit, and view the other end | 201 `{link}`; 422 |
| `DELETE /links/{linkID}` | edit either end | 204 |

- **Direction.** A link goes from its source to its target and reads from
  the source: "CHK-R7 derives from SYS-R2". The source is the more detailed
  end. A link's `direction` says which end the requirement asked about is,
  and `phrase` is the type's phrase read from that end.
- **The organization's link types**, seeded for every organization by the
  migration and when one is made:

  | Key | Forward | Backward | Acyclic | Suspect end | Counts for |
  |---|---|---|---|---|---|
  | `derives` | derives from | is derived into | yes | target | decomposition |
  | `refines` | refines | is refined by | yes | target | decomposition |
  | `satisfies` | satisfies | is satisfied by | yes | target | decomposition |
  | `verifies` | verifies | is verified by | yes | target | verification |
  | `conflicts` | conflicts with | conflicts with | no | both | nothing |
  | `relates` | relates to | relates to | no | none | nothing |

  `key` is stable and names a type in imports and scripts (`^[a-z][a-z0-9_]{1,39}$`,
  unique in the organization and the project). Names and phrases are at most
  40 and 60 characters.
- **Flags.** `acyclic` refuses a link that would come back around to its
  source over links of that type. `suspectEnd` says whose new revision makes
  the link suspect (#36), and `counts` what it counts for in coverage (#38).
  A type whose forward and backward phrases are equal is symmetric: A to B and
  B to A are one link.
- **Projects** use every organization type unless they disable one
  (`link_type_project.enabled`), and may make their own. `rules` limit a type
  in a project to pairs of requirement types `{sourceTypeId, targetTypeId}`,
  a null end meaning any. None means any pair. Rules bind links whose ends are
  both in the project; a link to another project is checked on its source's
  type alone.
- **Making a link** names the type by `typeId` or `typeKey`, the other end by
  `otherKey`, and `direction`: `outgoing` (the default) makes the requirement
  in the path the source. Both ends are requirements, never sections, out of
  the trash, and the type is enabled in the source's project and allows the
  pair. The database refuses a self-link (`trace_link_not_self`) and, for an
  acyclic type, a cycle (a trigger generalised from Armature's
  `00024_link_cycles.sql`, walking at most `trace.MaxCycleDepth` (64) links).
  Both are proven through SQL and answered 422 with a sentence naming the
  path, such as "CHK-R7 already derives from SYS-R2 through CHK-R3." A live
  link of the same type between the same ends is 422 "Those requirements are
  already linked that way." A requirement holds at most
  `trace.MaxLinksPerRequirement` (500) links.
- **Revisions on links.** A new link records the current revision of both
  ends as `source_reviewed_rev` and `target_reviewed_rev`, answered as
  `sourceRevision` and `targetRevision`. #36 compares them with the ends'
  revisions to say a link is suspect, and moves them on when it is cleared.
- **Removal** leaves a tombstone (`deleted_at`, `deleted_by`) that lists
  leave out and baselines (#37) read. Linking the two again makes a new link.
  Links are logged on both ends as `link` changes, never revisions, and emit
  `link.created` and `link.removed`.
- **Retire, rather than delete**, as #14: a type with links is retired, keeps
  its links and takes no new ones. `DELETE` of one is 409 `in_use`.
- UI: the links panel of a requirement, grouped by type and direction, with
  a requirement picker drawn from quick search (#20); the Link types tab of
  the project settings, and the organization's under Organization.

## #19 Labels

| Operation | Needs | Answers |
|---|---|---|
| `GET /requirements/{reqKey}/labels` | view | `{labels}`, names in order |
| `POST /requirements/{reqKey}/labels` | edit | `{labels}` after the add; 422 |
| `DELETE /requirements/{reqKey}/labels/{labelName}` | edit | 204, also when it did not carry it |
| `GET /labels?q&project&limit` | view, per requirement | `{labels}`: `{name, requirements}`, the most used first |
| `GET /labels/{labelName}/requirements?project&limit&offset` | view, per requirement | `{requirements, total, limit, offset}`, by key |

- **Stator's labels**, on requirements: a label is one word, trimmed, lower
  case, each run of spaces a hyphen, then only letters, digits, `-`, `_` and
  `.`, starting with a letter or a digit, at most `label.MaxNameLength` (40)
  characters (`label.Normalize`). A requirement carries at most
  `label.MaxPerRequirement` (50).
- **The organization's.** There is no list of labels to manage: a label
  exists while some requirement carries it, and is coined by being used. So
  autocomplete counts and offers only labels on requirements the caller may
  view, out of the trash.
- **Not normative.** Adding and removing are logged as `labels` changes and
  never write a revision. A copy takes its original's labels. A trashed
  requirement keeps its labels, and they come back with it.
- **Filtering** by label is part of search and the grid (#20).

## #20 Search, filters, grid and saved views

| Operation | Needs | Answers |
|---|---|---|
| `GET /search` | view, per hit | `{hits, total, limit, offset}`; 422 |
| `GET /search/quick?q&project&limit` | view, per hit | `{requirements}` |
| `GET /projects/{projectKey}/requirements` | view | `{rows, total, limit, offset}`; 422 |
| `GET /projects/{projectKey}/views` | view | `{views}`: the caller's, then the shared ones |
| `POST /projects/{projectKey}/views` | view; edit to share | 201 `{view}`; 422 |
| `PATCH /views/{viewID}`, `DELETE /views/{viewID}` | the owner, or administer for a shared one | `{view}`, 204 |

- **What is found**: the key, which typed whole (`CHK-R42`) ranks that
  requirement first, the title (weight A), the plain text of statement and
  rationale (B), and text and rich text attribute values (C). Only the current
  content is searched, never old revisions or anything in the trash, and only
  what the caller may view (`perm.ViewableRequirement`, in the SQL that finds
  the hits, so `total` counts what the caller sees).
- **Plain text** comes from `requirement_plain_text(jsonb)`, the SQL twin of
  `document.PlainText` as Stator's `page_plain_text` is, and feeds a generated
  search vector. The integration suite checks the two agree, so the index
  stays in step with the statement. The configuration `exciter_search` is
  Stator's `stator_search`: `simple` with `unaccent`, ignoring case and
  accents, without stemming.
- **Query syntax** (`q`, at most 200 characters, else 422 on `q`) is
  `websearch_to_tsquery`'s, as Stator's: every word must match, `"quoted
  words"` match as a phrase, `or` matches either side, `-word` leaves out
  what has it. An empty `q` lists by the filters alone, the latest change
  first.
- **Search filters**, across projects, each repeatable one matching any of
  its values: `project` (keys), `category`, `label`, `owner`,
  `updatedAfter`, `updatedBefore` (days in UTC, the second exclusive).
  `sort=updated` orders by the latest change. Highlighting is Stator's: `title`
  and `snippet` are runs of `{text, match}`, and the server never sends markup.
- **Quick search**, for the Ctrl K switcher: a `q` that starts like a key
  (`CHK-R4`) matches keys by prefix, and anything else matches when every word
  typed is a prefix of a word of the title. The best matches come first, then
  the latest change. `limit` is 1 to 20, 8 by default, and an empty `q`
  answers nothing.
- **The grid** is one project's requirements, narrowed by `q`, `kind`,
  `type`, `status`, `category`, `label`, `owner`, `priority` and `attr`. Each
  repeatable one matches any of its values, and different ones must all hold.
  `attr` is `attributeId:op:value`, at most `search.MaxAttributeFilters` (10):

  | Op | For | Matches |
  |---|---|---|
  | `is`, `not` | every kind | equal, or not: an option id, a user id, `true`, a number, a day, text without regard to case; a multi-select holding it |
  | `gte`, `lte` | number, date | at least, at most |
  | `contains` | text, rich text | holds the words given |
  | `set`, `unset` | every kind | has a value, or not; the value is left empty |

  An unknown attribute, an op its kind does not take or a value of the wrong
  shape is 422 on `attr`. `sort` names a column (`key`, `title`, `type`,
  `status`, `owner`, `priority`, `updated` or an attribute id), outline order
  when absent, and `desc` turns it round. `limit` is 1 to
  `search.MaxGridLimit` (500). Inline edits use `PATCH /requirements/{reqKey}`.
  A vitest test holds the filter builder to this syntax.
- **Views** are saved grids: `filter` (the same conditions as an object),
  `columns` (at most `search.MaxColumns` (30), from the sort columns plus
  `labels` and `outline`), and `sort`. A view is its owner's, and with
  `shared` it is offered to everybody who may view the project. Sharing
  needs edit. A shared view is changed or deleted by its owner or a project
  administrator. Names are unique per owner and project without regard to
  case, at most 100 characters.
- UI: `/search`, the Ctrl K switcher, and `/p/$projectKey/grid` with a column
  chooser, inline edit and the views menu.

## #21 Attachments

| Operation | Needs | Answers |
|---|---|---|
| `GET /requirements/{reqKey}/attachments` | view | `{attachments}`, the latest first |
| `POST /requirements/{reqKey}/attachments` | edit | 201 `{attachment}`; 413 `too_large`; 422 |
| `GET /attachments/{attachmentID}?inline` | view its requirement | the bytes |
| `DELETE /attachments/{attachmentID}` | edit its requirement | 204 |

- **Stator's attachments**, on requirements. A file goes through the API as
  one multipart part named `file`, at most `EXCITER_UPLOAD_LIMIT`. Over it the
  answer is 413 `too_large` with a sentence naming the limit, and an empty
  file is 422 on `file`. The content type sent is kept when specific, else the
  bytes are sniffed. Images report `width` and `height`.
- **Storage.** The object key is `org/<org>/requirement/<requirement>/<attachment>`,
  so a move to another project leaves the bytes where they are. The row
  guards the object: the database says a file exists and who may read it,
  and the bytes are served only through the API.
- **Download** answers `Content-Disposition: attachment` with the file name,
  `nosniff` and `private` caching. With `inline=true`, PNG, JPEG, GIF, WebP,
  PDF and plain text show in place; everything else, SVG included, downloads.
- **In the statement.** #21 adds two nodes to the allowlist, as Stator's #20
  did: a block `image` with `attachmentId`, `alt` (at most 500 characters)
  and `width` (pixels, or null), and an inline `attachment` with
  `attachmentId` and `fileName`.
- **A file a revision shows is never purged.** `referenced` says some
  revision's statement or rationale names it. Deleting such a file takes it
  off the list (`deleted_at`) and keeps the row and the bytes, so history
  still shows it. A trigger refuses deleting the row while a revision of a
  requirement that still exists names it, which the integration suite proves
  through SQL. A file nothing names is deleted, and its tombstone lets the
  worker remove the object. Purging the requirement removes its files with
  its revisions.
- **Not normative.** Uploads and deletions are logged as `attachment`
  changes. A copy copies the files and rewrites the copy's references.

## #22 Personal access tokens and MCP

| Operation | Needs | Answers |
|---|---|---|
| `GET /tokens` | member | `{tokens}`, the caller's, without secrets |
| `POST /tokens` | member, signed in with a session | 201 `{token}` with `secret`, once; 403 `session_only`; 422 |
| `DELETE /tokens/{tokenID}` | the owner | 204 |
| `GET /org/tokens`, `DELETE /org/tokens/{tokenID}` | admin | `{tokens}` with owners, 204 |
| `POST /mcp` | member | JSON-RPC 2.0 |

- **Stator's tokens**: the secret is `exciter_pat_` and 43 characters of
  base64url (32 random bytes). Only its SHA-256 is stored. It is sent as a
  bearer and wins over the cookie. A malformed token is refused before any
  lookup. `name` is at most 100 characters, and `expiresAt` must lie ahead.
  `lastUsedAt` moves at most once a minute.
- **Scopes.** None is every right the owner has, and `read` is read-only: a
  read-only token is refused every write but `POST /mcp` with 403
  `read_only_token`, and MCP hides and refuses its write tools.
- **Projects.** `projects` limits a token to up to 50 projects the maker may
  view, by key. A limited token is refused every `orgWide` operation of the
  table with 403 `projects_token`: "This token is limited to some projects,
  and this concerns the whole organization. Use a token without that limit,
  or sign in." Row level security confines it to its projects through
  `db.WithUserInProjects`. The limit cannot change after the token is made.
- **Changed:** `GET /auth/me` gains `token`, the calling token's id, name,
  scopes, projects and expiry, or null for a session.
- **MCP** is Stator's endpoint: protocol `2025-06-18`, server name
  `exciter`, `initialize`, `ping`, `tools/list`, `tools/call`,
  `resources/list` and `resources/read` (`exciter://openapi.json`). Its tools
  are drawn from the rows of the table that carry `tool` and `toolHelp`, and
  a pending operation cannot carry them. Whoever builds an operation below
  marks its row with the tool name given:

  | Tool | Operation |
  |---|---|
  | `list_projects`, `get_project` | `GET /projects`, `GET /projects/{projectKey}` |
  | `get_tree` | `GET /projects/{projectKey}/tree` |
  | `get_requirement` | `GET /requirements/{reqKey}` |
  | `search_requirements` | `GET /search` |
  | `list_requirements` | `GET /projects/{projectKey}/requirements` |
  | `create_requirement`, `update_requirement` | `POST /projects/{projectKey}/requirements`, `PATCH /requirements/{reqKey}` |
  | `list_revisions`, `get_revision`, `diff_revisions` | the history of #16 |
  | `list_links`, `link_requirements` | `GET` and `POST /requirements/{reqKey}/links` |
  | `list_transitions`, `transition_requirement` | `GET` and `POST /requirements/{reqKey}/transitions` |
  | `list_people` | `GET /people` |

- UI: `/tokens` under the account menu, and the organization's tokens for
  administrators.

## #23 CSV import and export

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/import` | administer | 201 `{import}`; 413 `too_large`; 422 |
| `GET /imports/{importID}` | its maker, or administer | `{import}` |
| `PUT /imports/{importID}/mapping` | its maker | `{import}`; 409 when it already started |
| `POST /imports/{importID}/start` | its maker | 202 `{import}`; 409 `import_problems` |
| `DELETE /imports/{importID}` | its maker, or administer | 204 |
| `GET /projects/{projectKey}/export.csv` | view | the file |

- **Upload** takes a CSV file, UTF-8 with or without a byte order mark,
  comma or semicolon separated, a header row first: at most
  `csvio.MaxImportBytes` (8 MB, else 413) and `csvio.MaxImportRows` (5000
  rows, else 422). It makes an import in state `uploaded` with the columns,
  their first `csvio.PreviewRows` values, and a mapping guessed from the
  headers. Nothing is written to the project yet.
- **Targets** a column maps to: `key` (updates the requirement of this
  project with that key; any other key is refused), `kind`, `title`,
  `statement`, `rationale` (plain text, a blank line between paragraphs),
  `type`, `status`, `owner`, `priority`, `labels` (several columns, or
  comma separated), `parent` (a key, of the project or of a row of the file)
  or `outline` (`2.3.1`, which builds the tree from the file's order), `links`
  (cells of `type:KEY`, separated by commas, the type by its key, the
  requirement in the cell the target), and `attr:<name>` for each attribute.
- **Unknown values are reported before anything is written.** The mapping
  answer lists `words` (type, status, priority and option values, and what
  each matched), `people` (matched by email to members who may view the
  project) and `problems`: every word that matched nothing, every person
  nobody answered for, a parent or link to a key that does not exist, a
  value of the wrong shape, a required value missing. `values` maps a word to
  a name the project has, and `people` chooses a member or nobody for a name.
  `start` is refused with 409 `import_problems` while any problem is left:
  "Some values in the file match nothing in the project. Map them, or change
  the file, before importing."
- **The worker** runs a started import after `import.requested`, in two
  passes: the rows, each in its own transaction, as revision 1 with the
  comment "Imported from <file>", then the tree and the links. `done` and
  `total` say how far it is, which the client polls, and a row that fails
  then goes to `refused` with its line and why. Cancelling stops it after the
  row it is on; what it made stays.
- **Export** writes the grid's rows (the same filter parameters) with the
  columns asked for (`column`, repeated, as the grid names them), keys kept,
  owners by email, links as `type:KEY` of the outgoing ones, statement and
  rationale as plain text, plus `outline` and `parent`. Exporting a project
  and importing the file into an empty one makes the same tree, content and
  links, which the integration suite proves. Past `csvio.MaxExportRows`
  (20000) rows the file ends with a note row and the answer carries
  `X-Export-Truncated: true`.
- UI: Import under the project settings, with Armature's `MappingCard` and
  `PeopleCard`, and Export in the grid's menu.

## #24 Project templates

| Operation | Needs | Answers |
|---|---|---|
| `GET /project-templates` | member | `{templates}` |

- **Built in**, in code (`internal/project/templates.go`), in the order
  offered. Every template uses the default statuses and workflow (#17):

  | Key | Types | Attributes | Sections | Link types |
  |---|---|---|---|---|
  | `system` | Stakeholder need, System requirement, Interface, Constraint | Source (text), Verification method (select: Test, Analysis, Inspection, Demonstration), Safety relevant (boolean) | Introduction, Stakeholder needs, System requirements, Interfaces, Constraints | all |
  | `software` | Functional, Non-functional, Interface, Use case | Component (text), Verification method, Release (text) | Introduction, Functional requirements, Non-functional requirements, Interfaces | all |
  | `test` | Test case, Test procedure | Preconditions (rich text), Expected result (rich text), Automated (boolean) | Test strategy, Test cases | verifies, relates |

- **Choosing** one is part of `POST /projects` (`template`). An unknown key is
  422 on `template`: "There is no project template called X. Pick one from
  the list, or leave it out for a blank project." The first type of a
  template is its default type. `project.template` keeps the key.
- The answer names what each template makes, for the chooser to show.
  Changing a template never changes the projects made from it.
- The integration suite makes a project from each built-in and checks that it
  is valid: every type has a workflow, every attribute's types exist, and
  every section is in the tree.
