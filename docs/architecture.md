# Architecture

Exciter is requirements management. It deliberately mirrors
[Armature](https://github.com/Cloudster1/Armature), the tracker, and
[Stator](https://github.com/praetorianer777/Stator), the wiki: same stack,
same patterns, same design tokens, same identity provider. Someone who knows
one codebase should find their way around the other two without a map.

This document describes the architecture as it is built. Each section names
the issue that brought it.

## Overview

```
browser --> web (nginx, React SPA) --> api (Go) --> PostgreSQL (primary + replica)
                                        |  +--> Valkey (cache of Armature and Stator answers, replay window)
                                        |  +--> S3-compatible storage (attachments, theme assets)
                                        |  +--> Armature API (as the viewing user)
                                        |  +--> Stator API (as the viewing user)
                                        +-- outbox --> worker (Go) --> mail, notifications, link sync to Armature and Stator, imports
Keycloak / any OIDC provider <-- login --+   (one realm for Exciter, Armature and Stator, so a person is one subject)
```

## Backend (`backend/`, #3)

- Module `github.com/cloudster1/exciter/backend`, Go 1.26.
- Binaries in `cmd/`: `api`, `worker`, `migrate`, `seed`, `openapi`, and the
  stand-ins `armature-stub` and `stator-stub` used by the test layers.
- One package per domain in `internal/`, each with a `Service`, a model and
  plain SQL. No ORM, no query generator: pgx v5 and hand-written SQL.
- Router: chi v5. A single route table drives the router, the OpenAPI
  document (`api/openapi.json`), the MCP tools at `POST /api/v1/mcp` and the
  consistency tests between them. An operation agreed before it is built
  carries `pending: true` and answers 501 `not_implemented`.
- Migrations: goose, embedded in the binary, applied by `cmd/migrate`. Each
  milestone's contract divides a range of migration numbers among its issues:
  `docs/api-contract-m1.md` for the requirements core (#10),
  `docs/api-contract-m2.md` for the Armature integration (#25),
  `docs/api-contract-m3.md` for review and control (#32),
  `docs/api-contract-m4.md` for the Stator integration (#39),
  `docs/api-contract-backlog.md` for what came after the first release,
  `docs/api-contract-m5.md` for traceability (#146),
  `docs/api-contract-m6.md` for the document view (#167), and
  `docs/api-contract-m7.md` for the MCP integration (#182).
- Configuration: environment variables only, prefix `EXCITER_`.
  `EXCITER_NAME` is what a first start calls the installation, `Exciter` by
  default; an administrator renames it under Settings, Installation.
- Files (#8): `internal/objectstore` keeps uploaded bytes in any bucket that
  speaks S3, signing its few calls itself with the standard library rather
  than an SDK. `EXCITER_S3_ENDPOINT`, `_BUCKET`, `_ACCESS_KEY`, `_SECRET_KEY`,
  `_REGION` and `_USE_SSL` name the bucket, which the api makes when missing;
  without an endpoint an upload is refused with the setting to fix. Every
  object lives under `org/<installation id>/`, where every file stored
  before #107 already was, and a row in the
  database says the object exists, so a removed row leaves at worst an
  unreachable object, never a broken link. `EXCITER_UPLOAD_LIMIT` caps an
  attachment (#21), 50 MB by default.
- PDF (#45): `internal/render` asks the render service, Stator's: a headless
  Chromium behind `render/server.mjs` that opens the web client's print view
  as the reader, with a read-only token made for that print, and prints it.
  `EXCITER_RENDER_URL`, `_TIMEOUT`, `_CONCURRENCY` and `_MAX_SIZE` name and
  bound it; without a URL a PDF download says export is off, and Word, which
  `internal/docx` writes in the api, works either way.
- Observability: `log/slog`, Prometheus metrics, OpenTelemetry traces.

### One installation

An installation serves one team (#82): nothing people or the API see names
an organization. The seed makes the installation on the first start under
`EXCITER_NAME`, migration `00400` refuses a database holding several
organizations, and `internal/installation` is the one way to its name and
default theme.

The schema says the same (#106 to #108). `installation` is one row, the
name and the default theme, and so are `oidc_provider` and
`armature_connection`, each held to one by a unique index on `(true)`. A
person's role and who decided it are columns of `app_user`, NULL for
somebody who asked to join and was not let in. No table carries `org_id`,
so project keys and the other names are unique in the installation, and a
transaction says only who acts. Tests on the catalog and on the Go source
refuse anything that names the organization again. What was sealed or
stored under the organization's id, the provider's client secret, Armature
tokens and the webhook secret, attachments and theme files, still is,
under the installation's id, so nothing stored earlier is lost.

Every table forces row level security. One permissive policy per table,
`<table>_installation`, lets every role at the rows, and the permission rules,
SQL functions used as restrictive policies for `exciter_app`, are the walls,
so a guard holds even for a query that forgot it. `app_user` shows the app
role only the people who belong to the installation, and the admin role
alone reads accounts, sessions, identities and sign-ins in flight, which the
app role holds no privilege on.

### Domains (M1 to M3)

- **Projects** (#11): a key such as `CHK`, settings, templates (#24). A key
  a project leaves stays in `project_key_alias` and keeps resolving, and
  triggers keep a key, current or former, to one project under a lock on it.
  `perm_project_holds(project, actor, permission)` is what every project
  policy asks. An archived project is read only, and only an archived one can
  be deleted, which the database holds too.
- **Permissions** (#12): `project_grant` gives a user, a group or everyone
  the role viewer, reviewer, editor or admin in a project, and a person holds
  the union of the rights of every grant that reaches them.
  `perm_project_holds` reads the grants; owners and administrators of the
  installation hold everything, everyone's grants leave guests out, and a
  guest holds at most editor. A trigger gives a new project its maker as
  admin and everyone as editor. The access inspector explains a verdict from
  the same function, so it says what the database enforces.
- **Requirements** (#13): key `CHK-R42`, a rich-text statement, rationale,
  type and attributes (#14), a place in the tree (#15), a status from the
  project's workflow (#17). A trigger hands out numbers from the project's
  counter under its row lock, and `perm_requirement_viewable` and
  `perm_requirement_editable` are the restrictive policies of every table
  that belongs to a requirement. Statements and rationales are documents of
  the shared editor, held to `internal/document`'s allowlist on the server
  and to the same table, as `api/document-allowlist.json`, in the web client.
- **Types and attributes** (#14): `requirement_type` and `attribute_def`
  belong to a project; a value lives in `requirement.attributes` under its
  attribute's id, GIN-indexed, and a trigger refuses a type or an attribute of
  another project. Every revision copies the type and the values it had, so
  what was ever used is retired rather than deleted; `type_usage` and
  `attribute_usage` count every use, whoever asks.
- **The tree** (#15): `requirement.parent_id` and `rank`, Stator's
  lexicographic positions in byte order; outline numbers are computed on read
  by one recursive query. A trigger under the project's tree lock refuses a
  loop or a parent of another project, and another renumbers a requirement
  moved to another project and keeps its old key, so a move holds whoever
  makes it.
- **Reuse** (#49): a library project's requirement copied into another
  project follows its source. `requirement.reuse_source`, `reuse_revision`
  and `reuse_state` say what it follows, from which revision, and whether
  its words are the source's or a variant's own; `requirement_reuse_guard`
  holds a following copy's words to the source's at its latest revision, so
  a take is the only way they change. The copy keeps its words locally, so
  search, baselines, export and coverage read it as any requirement.
- **Workflow** (#17): `req_status`, `workflow`, its steps, transitions and
  rules belong to a project, which follows one workflow while a type may
  follow another. `internal/workflow` decides what a reader is offered and
  what a transition asks, Armature's engine without schemes; the database
  moves the status. `exciter_app` holds no `UPDATE` on `requirement.status_id`,
  so it changes only through `requirement_transition`, which checks the
  transition leaves the current status and is the caller's to take, logs it
  and runs the owner post-functions, and `requirement_remap_status`, which
  only takes a status the workflow lacks to one it has. A step may send a
  requirement elsewhere on a new revision, which is how an edit supersedes an
  approval. The designer is Armature's, drawn with React Flow.
- **Search** (#20): Stator's full-text search on requirements. The
  configuration `exciter_search` ignores case and accents and does not stem;
  a generated `requirement.search_vector` weighs the title A, the plain text
  of statement and rationale B and text values C, the plain text coming from
  `requirement_plain_text`, the SQL twin of `document.PlainText`.
  `requirement_search_rows` matches through the index and checks rights once
  per project. The grid is one project's requirements in outline order,
  narrowed and ordered in SQL built from checked parts, never from text a
  person wrote. Saved views keep a grid's filter, columns and order, their
  owner's and shared by editors, under `saved_view`'s own policies.
- **Outbound webhooks** (#48): Stator's `internal/webhook`. The worker queues a
  `webhook_delivery` for every endpoint on that hears an event's topic and
  project, ahead of the topic's other handlers, and a sender posts what is
  due, signed with the endpoint's sealed secret, its payload read as the
  endpoint's owner, retried with backoff and turned off after a day of
  failing. The `event` webhook of the API document describes what is sent.
- **Query language** (#47): Armature's NQL as `internal/rql`, compiled per
  project from a catalog of the fixed fields, the attributes by quoted name
  and a field per end of each link type, to a WHERE clause over the grid's
  requirement with every value bound. Coverage levels and keys are read by
  `requirement_coverage` and `requirement_key_id`, as the reader sees; the
  grid, its views, CSV export, coverage and the matrix take it through
  `search.Filter.Query`, and `/requirements/suggest` completes it.
- **Labels** (#19): Stator's labels on requirements. `requirement_label` holds
  a name per requirement and nothing else, so a label exists while something
  carries it and is everyone's, not a project's. Its rows follow the
  requirement's policies: seen by who may view it, put on and taken off by who
  edits it, never updated in place. Suggestions and a label's list count only
  requirements the reader may view, out of the trash.
- **Revisions** (#13, #16): a save that changes the normative content writes
  an immutable revision; status, owner, labels and position are logged but
  never revisioned. Reviews, baselines and links point at revision numbers.
- **History** (#16): the revisions of a requirement compare block by block
  with Stator's document diff, which marks changed words with `diffInsert`
  and `diffDelete`, marks only a comparison carries; values compare as a
  reader sees them. A restore writes the old content as the next revision,
  and the activity merges revisions with the changes that wrote none.
- **Trace links** (#18): typed and directed links between requirements,
  read from the source. `link_type` holds the types for all projects, seeded
  with the installation, and the projects' own; `link_type_project` turns
  one off in a project and `link_type_rule` limits it to pairs of
  requirement types. `trace_link` records the revision of each end and is
  tombstoned, never deleted, through the app role. A trigger refuses a link
  between anything but two requirements out of the trash, a type the
  source's project may not use, a repeat, an end's 501st link and, for an
  acyclic type, a circle, walking every link whoever writes; another logs
  the link on both ends. A link shows to whoever views both ends. It turns
  suspect when the end its type names has a newer revision (#36).
- **Attachments** (#21): Stator's files, on requirements. The bytes live in
  the bucket under `org/<installation id>/requirement/<requirement>/<id>`, a
  key a trigger gives the row, and are served only through the API; the `attachment` row is
  what row level security guards, seen with the requirement and written by
  who edits it. A statement shows a file by an `image` or `attachment` node.
  A file some revision of its requirement shows is only taken off the list,
  and a trigger refuses deleting it while the requirement exists. Deleting a
  row leaves a tombstone, whichever path deletes it, and the worker's reaper
  removes the bytes behind each.
- **Tokens and MCP** (#22): Stator's personal access tokens, `exciter_pat_`
  and 43 characters, stored as their SHA-256 and sent as a bearer. A token
  acts as its owner; a read-only one is refused every
  write before any handler, and one limited to projects carries them into
  every transaction (`app.token_projects`), where the permission functions
  read them. `POST /mcp` serves the table's tool-marked rows to assistants,
  each call run through the whole router as the caller, in MCP `2025-11-25`
  or `2025-06-18` (#183), each tool with the schema of its answer and its
  hints drawn from the row. A resource reads the document of #45 the same
  way and writes it as Markdown (`specdoc.Markdown`), cut between entries;
  the prompts are Go beside the tools (#187).
- **CSV import and export** (#23): Armature's import, reshaped for
  requirements. An upload is kept whole in `import_job` with a mapping
  guessed from its headers; the same analysis reads it against the project
  for the mapping step and for the worker, so the import that runs is the
  one that was shown. After `import.requested` the worker runs it as the
  person who started it, each row in its own transaction, then the tree and
  the links. Export is the grid's query written as CSV.
- **ReqIF import and export** (#44): the exchange format of requirements
  tools. An upload is read once into `reqif_import`, the files its rich text
  shows beside it, and analysed against the project the way a CSV import is:
  types, attributes and link types match by the identifier an earlier
  exchange tied, then by name, and the rest are made. The worker makes the
  schema, then writes each object as the person who started it, then the
  relations, and ties every identifier in `reqif_identity`, which the export
  writes back. Rich text is XHTML converted to and from the editor's
  documents. See `docs/api-contract-backlog.md`.
- **Documents** (#45): a project's specification, a section with what is
  below it, or a baseline as it was taken, read by `internal/specdoc` as one
  document: a cover, the contents, and each entry with its text, a table of
  its fields and attributes, and its links. The print view in the web client
  draws it and the render service prints that as PDF; `internal/docx`,
  Stator's Word writer cut to a requirement's blocks, writes it as Word.
- **Markdown** (#186): `internal/markdown`, Stator's export and import over
  goldmark, reads and writes statements, rationales and comments as
  Markdown beside the stored document. A read with `format=markdown` adds
  each where Markdown holds it, and a write converts the Markdown fields
  before the document's own checks, refusing what would not come back as
  written. `docs/markdown.md` lists the spelling.
- **Project templates** (#24): three built-in structures in code, shaped
  after Stator's space templates. Choosing one is part of making a project:
  in the same transaction it sets the types, attributes, link types and
  starter sections, copied once, so a later change to a template leaves the
  projects made from it alone.
- **Review and approval** (#35), **baselines** (#37), **coverage** (#38).
- **Traceability** (M5, `docs/api-contract-m5.md`): the matrix of #38 in
  `internal/trace/matrix.go`, its rows those `search.Keep` keeps in the
  grid's order, its columns the requirements or Armature issues they reach
  through the link types, direction and relations asked for. Its web page,
  `/p/$projectKey/matrix`, keeps the whole layout in its address. A
  project's trace model (`trace_level` and its types and steps, replaced
  whole) names the levels its chain report (`internal/trace/chain_report.go`)
  follows from each root, one query per level for the whole page.
  `internal/xlsx` writes the matrix, the chains and a baseline's matrix as
  a workbook by hand, as `internal/docx` writes Word.
- **Document view** (M6, `docs/api-contract-m6.md`, planned): the
  specification of #15 written, moved through its workflow and reviewed in
  place. Its changes, filters and review mode are parameters of
  `GET /projects/{projectKey}/spec`; bulk transitions and review requests
  take each requirement through the single operation, each on its own.
- **MCP integration** (M7, `docs/api-contract-m7.md`, planned): the endpoint
  of #22 on the current protocol, with tools for everything since M1 but
  deciding a review, requirements in Markdown, the specification as
  resources, and prompts. Exciter becomes the OAuth authorization server of
  its MCP endpoint (`internal/oauth`), whose tokens are good for MCP alone,
  and what is written through a token or a grant carries `via`.
- **Signed approvals** (#46): in a project with `settings.signedApprovals`,
  a decision on a review is an electronic signature. The reviewer confirms a
  meaning the server writes and proves who they are again, with their
  password or a fresh sign-in at the identity provider
  (`/auth/oidc/reauthenticate`, `prompt=login`), which leaves a single-use
  `reauthentication` of their session. `review_signature` keeps the meaning,
  the revision and its content hash, and how the signer proved it; the
  triggers `review_signature_guard` and `review_decision_signed` refuse a
  signature without a fresh proof and a decision without a signature.

### Themes (#8)

- `internal/theme` keeps a person's themes: a spec of overridden colour
  tokens, fonts, radii, shadows, cursors, icons, a backdrop, a moving
  picture and extra CSS, validated as Armature validates it, so nothing in a
  theme can load from another site; its pictures and fonts sit in the object
  store under `org/<installation id>/theme/<theme>/`. `armature-theme/1` packs a theme
  with its files as one JSON file; an import gives the files new ids and
  rewrites the spec and the CSS to name them. The examples and Armature's
  own theme files (`internal/theme/testdata`, byte for byte) are tested to
  validate.
- **One family** (#59): `internal/theme/testdata` also holds a theme exported
  from a running Armature and one from a running Stator, each pinned by its
  hash and its commit, and Exciter's own export, which
  `TestExcitersExportIsTheCommittedFixture` keeps current
  (`EXCITER_UPDATE_FIXTURES=1`). Each imports and exports again the same.
  `make theme-tokens` vendors the token names of the other two to
  `api/{armature,stator}/theme-tokens.json`, and a vitest test holds
  Exciter's list to both.
- `theme` is its maker's: through the app role a person sees their own
  themes and the shared ones, changes their own or, as an administrator, a
  shared one, and never takes one over. `user_theme` is each person's
  choice, of a theme they may see or of the built-in theme;
  `installation.default_theme_id` is the shared theme everybody sees until they
  choose, which only administrators name, and which a theme taken private
  stops being at once.

### Events and the audit log (#4)

- `events.Emit(ctx, tx, topic, payload)` writes to `outbox_event` in the
  caller's transaction. The app role may only insert, and only events whose
  `actorId` is the person the transaction acts for.
- `cmd/worker` claims events with `FOR UPDATE SKIP LOCKED` in a transaction of
  its own, handles them by topic through `events.Mux`, retries a failure
  after a growing delay and after `MaxAttempts` marks it done with an
  `event.dead_lettered` entry in the audit log.
- `audit_log` is written with the act, read only by owners and
  administrators, never updated or deleted, and pruned by `audit_log_prune`
  after `EXCITER_RETAIN_AUDIT`.

### Identity (#5, #7)

- People belong to the installation (`app_user.role`): owner, admin,
  member or guest. Letting somebody go switches
  the account off (`is_active`), which ends their sessions and tokens and
  keeps their account and work for switching them on (#86). Through the app
  role only an administrator changes a role or switches somebody off, nobody
  becomes or stops being an owner, and nobody leaves, neither by losing the
  role nor by the account being deleted (`app_user_role_guard`); what only a
  member may hold, a token, a grant or a requirement, a trigger keeps to
  members (`held_by_a_member`). The database keeps an active administrator
  (`app_user_keeps_an_administrator`), taking turns on an advisory lock so two
  administrators cannot switch each other off at once.
- A sign-in opens a `user_session` whose secret lives only in an http-only
  cookie; the row holds its SHA-256. It lands in the installation (#85): the
  session keeps how it was proven, written once
  (`session_keeps_its_opening`), and nothing switches it. Single
  sign-on starts at `/auth/oidc/start` with the installation's provider, and
  `/auth/me` answers the installation's name and the caller's role.
- The installation has one OIDC provider (`oidc_provider`), its client
  secret sealed with `EXCITER_SECRET_KEY`. `internal/oidc` runs the
  authorization code flow with PKCE, a nonce and a single-use state
  (`oidc_login`), verifies the id token against the issuer's keys, and
  matches a person by issuer and subject (`user_identity`), or by a verified
  address on their first sign-in. `EXCITER_OIDC_BACKCHANNEL` lets the api
  reach a provider at another address than the browser does, as in compose.
- Provider groups are synced into `groups` at every sign-in;
  `oidc_group_role` maps a group to admin or member, and
  `app_user.role_source` says whether a role follows the provider. A
  mapping never moves an owner or a guest. Somebody the provider vouches for
  who is not a member is refused and noted in `join_request`, where an
  administrator lets them in or turns them away. Through the app role only
  administrators read or change the provider, the mapping and the requests.
- `authenticate` puts the principal and the person it acts for on every
  request, so row level security sees who acts; `requireAuth` and
  `requireAdmin` guard the routes. Everybody signed in belongs to the
  installation (#84): a password sign-in of somebody without a membership is
  refused with "Your account has been switched off. Ask an administrator.",
  and a session whose membership went ends, so the browser goes back to sign
  in and hears it there. Behind a proxy in
  `EXCITER_TRUSTED_PROXIES` the client's address is the one it names in
  `X-Real-IP`.

## Frontend (`web/`, #6)

React 19, Vite, TypeScript strict, TanStack Router and Query, Tailwind v4,
the `components/ui` kit and the Clay tokens of Armature's
`docs/design/01-tokens.md`. Strings live in `web/src/i18n`, English and
German.

- `api/client.ts` is the one place that calls the API, typed by
  `api/schema.d.ts`, which `make web-schema` generates from
  `api/openapi.json`. A 401 from any request sends the reader to `/login` and
  back afterwards.
- Routes are defined in code (`routes/index.ts`); everything but `/login`
  sits in the shell (`features/shell`): a rail, a sidebar that becomes a
  drawer below the `shell` breakpoint, a top bar with breadcrumbs, the
  account menu, and the Ctrl K quick switcher. The places
  of a build are listed once, in `features/shell/places.ts`.
- Theming (#8): `styles/index.css` holds the tokens, light and dark, which
  follow `prefers-color-scheme` unless the rail's theme button stamps
  `data-theme` on the root; `lib/theme.ts` remembers the choice. A custom
  theme compiles (`lib/theme-css.ts`, Armature's compiler) to one unlayered
  stylesheet that `features/themes/ThemeLoader` puts in the head, cached so
  a reload paints it before the server answers, and `BackdropEffect` draws
  the moving picture a theme asks for. `lib/theme-tokens.ts` lists the tokens
  the editor offers, which a test holds equal to the stylesheet's.
  `styles/ratchet.test.ts` refuses a hand-typed size, colour or control
  outside the kit, the stylesheet and `config.ts`.
- `deploy/Dockerfile.web` builds the static files into nginx, which proxies
  `/api`, `/healthz` and `/readyz` to the api, so the browser sees one origin.
- `GET /api/v1/meta` answers anybody with the product, the build, the API
  version and the integration capabilities, as Stator and Armature answer
  theirs (#99). The build is stamped from `scripts/build-stamp.sh`, which the
  Makefile runs once and compose passes to the backend image.

## Armature and Stator integration (M2, M4)

- **Identity**: one OIDC realm serves all three products (#2, #7).
- **Connection**: an administrator sets the Armature or Stator address; each
  person stores their own token, sealed at rest. Every call is made as the
  viewing person through the SSRF guard (#26, #40).
- **Connecting Armature** (#26): `internal/armature` holds the client, the
  per-token cache in Valkey and the service, Stator's. Saving the address
  asks Armature's `/meta` who answers and what it offers (#100), through
  `internal/netguard`, which
  refuses any host inside the network that `EXCITER_OUTBOUND_ALLOW` does not
  name; `EXCITER_ARMATURE_BACKCHANNEL` reaches a public address at another
  inside the stack. A token is sealed with `EXCITER_SECRET_KEY` bound to the
  installation and the person, and the database lets `exciter_app` read only
  the actor's own token row and administrators alone the connection; moving
  the connection forgets every token in a trigger. `Service.Viewer` is what
  #27 to #30 call Armature through; its `Offered` is what each feature checks
  for its capability before it calls (`docs/integration.md`).
- **Connecting Stator** (#40): `internal/stator` is the same again: the
  client reaches Stator through the same guard, with
  `EXCITER_STATOR_BACKCHANNEL` for the stack, and saving the address asks
  Stator's `/meta` what answers and whether it takes remote links on pages,
  which the admin role records on the connection. Tokens and the connection
  are walled in the database as Armature's are.
- **Trace to issues** (#27): a requirement links to issues as implements,
  verifies or relates. The worker puts a remote link with source `Exciter`
  on each issue; Armature's webhooks keep the stored status category current
  (#29).
- **Link sync** (#27): database triggers write `armature.links` for a
  requirement whose links, revision, title, key, project or trash changed,
  in the changer's name; the worker's `armature.LinkSync` plans the remote
  links one requirement should have against `armature_remote_link` and puts
  or takes off the difference as that person, so Armature names them as the
  link's creator. Chips read issues live with the viewer's token, and every
  such read stores the status category for everyone.
- **Trace to pages** (#41, #42): the same, against Stator pages, with the
  page's latest version stored for everyone so a link whose page moved on is
  suspect. `internal/stator` reads pages and Stator's quick search as the
  viewer through the same per-token cache in Valkey, and its receiver
  follows `page.published`, `page.moved` and `page.deleted`. The way back is
  Armature's link sync: `stator.links`, written by triggers, has the worker
  put one remote link per requirement and page as the person whose change
  it was, recorded in `stator_remote_link`, once Stator reports
  `page-remote-links@1` on `/meta`; `page.remote_link.removed` leaves a link
  a person took off detached.
- **Stator's API** is vendored at `api/stator/openapi.json`, its commit in
  `api/stator/SOURCE`, refreshed with `make stator-openapi REF=`. The
  `stator-stub` answers in its shapes, and a contract test holds it to them
  (#39, #40).
- **Armature's API** is vendored at `api/armature/openapi.json`, its commit in
  `api/armature/SOURCE`, refreshed with `make armature-openapi REF=`. The
  `armature-stub` answers in its shapes, and a contract test holds it to them
  (#25, #26).

## Testing

`./run-tests.sh` runs every layer in order; CI runs the same layers split
across jobs (`.github/workflows/ci.yml`), and `tests/test-ci.sh` checks the
jobs cover them all.

| Layer | What | Since |
|---|---|---|
| shell | release, release workflow, chart, CI and branch-guard tests | #1 |
| check-format | biome over `web/` and `e2e/` | #6 |
| check-go | gofmt, vet, Go unit tests with the race detector, OpenAPI drift | #3 |
| check-web | lint, types, vitest, API types drift | #6 |
| test-integration | Go against the compose stack, every response validated against `api/openapi.json` | #3 |

The integration suite (`backend/test`) works in the stack's one installation,
one test at a time (#90). A test keeps apart by what it makes: owners and
people with addresses of their own (`h.owner`, `h.addPerson`, `h.outsider`),
projects under keys with the test's mark (`projectKey`), groups and names
with a suffix, and an Armature stub tenant of its own. When it ends its
projects go, newest first, and then its people. It asserts on what it made,
never on totals the whole installation shares, and reads shared records such
as the audit log by its own actor or since it started. A test that changes
something there is one of (the link types for all projects, the default
theme, the Armature connection, the identity provider and its people, the
installation's name) puts it back through a keeper (`keepLinkTypes` and the
like), and `installationWide` lists each with its reason, which
`TestInstallationWideTestsAreListedAndRunAlone` holds to the code. The rules
of the last active administrator need an installation with one, so those tests
migrate a database of their own.
| test-e2e | Playwright against the compose stack, with axe | #9 |

The browser suite (#9, `e2e/`, run by `mk/e2e.mk`) drives Chromium in
Microsoft's Playwright image of the exact version `e2e/package.json` pins, on
the host's network so it reaches the stack as a person at this machine does,
at desktop size and at 360x740. It reads the ports from `.cache/stack.env`. A
setup project signs alice and bob in to the installation through Keycloak once
and keeps their sessions in `e2e/.auth/`, and setup and teardown leave it
showing the built-in theme. Every spec shares the installation and keeps to
its own: with
`peopleTest` from `e2e/fixtures/people.ts`, each spec file gets per worker an
alice who administers and a bob who is a member, made by the bootstrap owner
and signed in with a password, on the built-in theme, and let go when the
worker stops; its projects carry keys from `uniqueKey`, which no other worker
or run draws. The specs that change what there is one of, the provider, the
default theme and the Armature connection, run in the `serial` project of one
worker beside the rest. What no endpoint makes, such as an unreachable
provider or a page of audit entries, a spec arranges straight in the database
through `e2e/fixtures/db.ts` and puts back again. `make test-e2e ONLY=<grep>` runs some
of it, `SHARD=1/2` a part as CI does, and `make e2e-report` serves the last
report.

## Deployment

Docker Compose for development (`deploy/docker-compose.yml`, run through
`mk/stack.mk`, #2), and the Helm chart `deploy/charts/exciter` everywhere
else (#104, `docs/deployment.md`): Stator's chart, with the api, worker and
web Deployments, the roles, secrets, migrate, bootstrap and seed Jobs, and
Postgres through CloudNativePG or bundled, Valkey and SeaweedFS for a trial.
`armature.url` and `stator.url` let the bootstrap connect the partners. A
release builds the `backend`, `web` and `render` images from its tag and pushes them to
`ghcr.io/cloudster1/exciter-<image>`, and the chart to
`oci://ghcr.io/cloudster1/charts/exciter` (`docs/releasing.md`, #101).

- Every checkout gets a compose project `exciter-<hash of its path>` and a
  slot of twenty host ports from `scripts/stack-ports.sh`, all bound to
  `127.0.0.1`: web +0, api +1, Keycloak +2, Mailpit +3, Postgres +4, its
  replica +5, Valkey +6, S3 +7, `armature-stub` +8, `stator-stub` +9.
- Postgres runs as a primary with a genuine streaming replica. The init script
  creates `exciter_app`, bound by every row level security policy,
  `exciter_admin`, exempt by policy rather than as superuser, and the
  replication role.
- Keycloak imports `deploy/keycloak/realm.json` on every start: realm
  `dynamo` with clients `exciter`, `armature` and `stator` (a `groups`
  claim, and PKCE for the two that send it; Armature does not yet) and the
  development people. The file is the suite's, the same in Armature and
  Stator; `make realm-check`, in the shell layer, refuses one that drifts
  from Dynamo's copy (#102).
- The api keeps its files in SeaweedFS's bucket `exciter-files`; the
  integration suite writes to `exciter-test`, so what it leaves never shows
  among the app's files.
- `migrate` applies the embedded migrations as the database owner, `seed`
  makes the installation with its owner, provider and development people, and
  `api` serves on `API_PORT` once both are done (#3). `worker` drains the outbox and prunes the audit log (#4). `web`
  serves the client on `WEB_PORT` once the api is healthy (#6). `render`
  prints the print view as PDF once the web is healthy (#45); it publishes no
  port, and the api and the integration suite reach it at `render:8090`.
