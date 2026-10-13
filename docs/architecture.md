# Architecture

Exciter is requirements management. It deliberately mirrors
[Armature](https://github.com/Cloudster1/Armature), the tracker, and
[Stator](https://github.com/praetorianer777/Stator), the wiki: same stack,
same patterns, same design tokens, same identity provider. Someone who knows
one codebase should find their way around the other two without a map.

This document describes the architecture the milestones build towards. Each
section names the issue that brings it; until then the section is the plan.

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
  `docs/api-contract-m1.md` for the requirements core (#10).
- Configuration: environment variables only, prefix `EXCITER_`.
- Files (#8): `internal/objectstore` keeps uploaded bytes in any bucket that
  speaks S3, signing its few calls itself with the standard library rather
  than an SDK. `EXCITER_S3_ENDPOINT`, `_BUCKET`, `_ACCESS_KEY`, `_SECRET_KEY`,
  `_REGION` and `_USE_SSL` name the bucket, which the api makes when missing;
  without an endpoint an upload is refused with the setting to fix. Every
  object of an organization lives under `org/<org>/`, and a row in the
  database says the object exists, so a removed row leaves at worst an
  unreachable object, never a broken link. `EXCITER_UPLOAD_LIMIT` caps an
  attachment (#21), 50 MB by default.
- Observability: `log/slog`, Prometheus metrics, OpenTelemetry traces.

### Multi-tenancy

Every table has `org_id` and a primary key `(org_id, id)` with composite
foreign keys, and FORCE ROW LEVEL SECURITY on `current_org_id()`. The
application connects as `exciter_app`, which row level security binds;
permission rules are SQL functions used as restrictive policies, so a guard
holds even for a query that forgot it.

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
  organization hold everything, everyone's grants leave guests out, and a
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
- **Labels** (#19): Stator's labels on requirements. `requirement_label` holds
  a name per requirement and nothing else, so a label exists while something
  carries it and is the organization's, not a project's. Its rows follow the
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
  read from the source. `link_type` holds the organization's types, seeded
  for every organization, and the projects' own; `link_type_project` turns
  one off in a project and `link_type_rule` limits it to pairs of
  requirement types. `trace_link` records the revision of each end and is
  tombstoned, never deleted, through the app role. A trigger refuses a link
  between anything but two requirements out of the trash, a type the
  source's project may not use, a repeat, an end's 501st link and, for an
  acyclic type, a circle, walking every link whoever writes; another logs
  the link on both ends. A link shows to whoever views both ends. It turns
  suspect when the end its type names has a newer revision (#36).
- **Attachments** (#21): Stator's files, on requirements. The bytes live in
  the bucket under `org/<org>/requirement/<requirement>/<id>`, a key derived
  from the row, and are served only through the API; the `attachment` row is
  what row level security guards, seen with the requirement and written by
  who edits it. A statement shows a file by an `image` or `attachment` node.
  A file some revision of its requirement shows is only taken off the list,
  and a trigger refuses deleting it while the requirement exists. Deleting a
  row leaves a tombstone, whichever path deletes it, and the worker's reaper
  removes the bytes behind each.
- **Tokens and MCP** (#22): Stator's personal access tokens, `exciter_pat_`
  and 43 characters, stored as their SHA-256 and sent as a bearer. A token
  acts as its owner in one organization; a read-only one is refused every
  write before any handler, and one limited to projects carries them into
  every transaction (`app.token_projects`), where the permission functions
  read them. `POST /mcp` serves the table's tool-marked rows to assistants,
  each call run through the whole router as the caller.
- **CSV import and export** (#23): Armature's import, reshaped for
  requirements. An upload is kept whole in `import_job` with a mapping
  guessed from its headers; the same analysis reads it against the project
  for the mapping step and for the worker, so the import that runs is the
  one that was shown. After `import.requested` the worker runs it as the
  person who started it, each row in its own transaction, then the tree and
  the links. Export is the grid's query written as CSV.
- **Project templates** (#24): three built-in structures in code, shaped
  after Stator's space templates. Choosing one is part of making a project:
  in the same transaction it sets the types, attributes, link types and
  starter sections, copied once, so a later change to a template leaves the
  projects made from it alone.
- **Review and approval** (#35), **baselines** (#37), **coverage** (#38).

### Themes (#8)

- `internal/theme` keeps a person's themes: a spec of overridden colour
  tokens, fonts, radii, shadows, cursors, icons, a backdrop, a moving
  picture and extra CSS, validated as Armature validates it, so nothing in a
  theme can load from another site; its pictures and fonts sit in the object
  store under `org/<org>/theme/<theme>/`. `armature-theme/1` packs a theme
  with its files as one JSON file; an import gives the files new ids and
  rewrites the spec and the CSS to name them. The examples and Armature's
  own theme files (`internal/theme/testdata`, byte for byte) are tested to
  validate.
- `theme` is its maker's: through the app role a person sees their own
  themes and the shared ones, changes their own or, as an administrator, a
  shared one, and never takes one over. `user_theme` is each person's
  choice, of a theme they may see or of the built-in theme;
  `org.default_theme_id` is the shared theme everybody sees until they
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

- People are global (`app_user`); `org_member` gives each a role per
  organization: owner, admin, member or guest. Through the app role only an
  owner or admin changes memberships, nobody becomes an owner, and the
  database keeps an organization's last owner.
- A sign-in opens a `user_session` whose secret lives only in an http-only
  cookie; the row holds its SHA-256. A session acts in one organization at a
  time and switches only where its proof reaches (`session_reaches`): a
  password everywhere the person is a member, a provider's sign-in in the
  organization it was made for, the ones whose enabled provider has the same
  issuer, and the ones the person owns.
- Each organization may have one OIDC provider (`oidc_provider`), its client
  secret sealed with `EXCITER_SECRET_KEY`. `internal/oidc` runs the
  authorization code flow with PKCE, a nonce and a single-use state
  (`oidc_login`), verifies the id token against the issuer's keys, and
  matches a person by issuer and subject (`user_identity`), or by a verified
  address on their first sign-in. `EXCITER_OIDC_BACKCHANNEL` lets the api
  reach a provider at another address than the browser does, as in compose.
- Provider groups are synced into `groups` at every sign-in;
  `oidc_group_role` maps a group to admin or member, and
  `org_member.role_source` says whether a role follows the provider. A
  mapping never moves an owner or a guest. Somebody the provider vouches for
  who is not a member is refused and noted in `org_join_request`, where an
  administrator lets them in or turns them away. Through the app role only
  administrators read or change the provider, the mapping and the requests.
- `authenticate` puts the principal, its organization and the person it acts
  for on every request, so row level security sees both; `requireAuth`,
  `requireOrg` and `requireAdmin` guard the routes. Behind a proxy in
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
  account and organization menus, and the Ctrl K quick switcher. The places
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

## Armature and Stator integration (M2, M4)

- **Identity**: one OIDC realm serves all three products (#2, #7).
- **Connection**: an org admin sets the Armature or Stator address; each
  person stores their own token, sealed at rest. Every call is made as the
  viewing person through the SSRF guard (#26, #40).
- **Trace to issues** (#27): a requirement links to issues as implements,
  verifies or relates. The worker puts a remote link with source `Exciter`
  on each issue; Armature's webhooks keep the stored status category current
  (#29).
- **Trace to pages** (#41, #42): the same, against Stator pages.

## Testing

`./run-tests.sh` runs every layer in order; CI runs the same layers split
across jobs (`.github/workflows/ci.yml`), and `tests/test-ci.sh` checks the
jobs cover them all.

| Layer | What | Since |
|---|---|---|
| shell | release, CI and branch-guard tests | #1 |
| check-format | biome over `web/` and `e2e/` | #6 |
| check-go | gofmt, vet, Go unit tests with the race detector, OpenAPI drift | #3 |
| check-web | lint, types, vitest, API types drift | #6 |
| test-integration | Go against the compose stack, every response validated against `api/openapi.json` | #3 |
| test-e2e | Playwright against the compose stack, with axe | #9 |

The browser suite (#9, `e2e/`, run by `mk/e2e.mk`) drives Chromium in
Microsoft's Playwright image of the exact version `e2e/package.json` pins, on
the host's network so it reaches the stack as a person at this machine does,
at desktop size and at 360x740. It reads the ports from `.cache/stack.env`. A
setup project signs alice and bob in to `demo` through Keycloak once and keeps
their sessions in `e2e/.auth/`, and setup and teardown leave `demo` showing
the built-in theme. A spec that changes what an organization shows uses
`orgTest` from `e2e/fixtures/org.ts`: each spec file gets an organization of
its own per worker, made through the api's test endpoints, `POST` and
`DELETE /api/v1/test/orgs`, with the bootstrap administrator as its owner,
alice and bob let in and the stack's Keycloak as its provider, and removed
with all its rows and files when the worker stops. The endpoints are routed
only when `EXCITER_TEST_ENDPOINTS` is on, which compose sets for the api and a
deployment never does, ask for `EXCITER_TEST_ENDPOINTS_TOKEN` in
`X-Exciter-Test-Token`, and are left out of `api/openapi.json`. What no
endpoint makes, such as an organization whose provider is down or a page of
audit entries, a spec arranges straight in the database through
`e2e/fixtures/db.ts` and removes again. `make test-e2e ONLY=<grep>` runs some
of it, `SHARD=1/2` a part as CI does, and `make e2e-report` serves the last
report.

## Deployment

Docker Compose for development (`deploy/docker-compose.yml`, run through
`mk/stack.mk`, #2); a Helm chart follows the first release.

- Every checkout gets a compose project `exciter-<hash of its path>` and a
  slot of twenty host ports from `scripts/stack-ports.sh`, all bound to
  `127.0.0.1`: web +0, api +1, Keycloak +2, Mailpit +3, Postgres +4, its
  replica +5, Valkey +6, S3 +7, `armature-stub` +8, `stator-stub` +9.
- Postgres runs as a primary with a genuine streaming replica. The init script
  creates `exciter_app`, bound by every row level security policy,
  `exciter_admin`, exempt by policy rather than as superuser, and the
  replication role.
- Keycloak imports `deploy/keycloak/realm.json` on every start: realm
  `suite-dev` with clients `exciter`, `armature` and `stator` (PKCE, a
  `groups` claim) and the development people.
- The api keeps its files in SeaweedFS's bucket `exciter-files`; the
  integration suite writes to `exciter-test`, so what it leaves never shows
  among the app's files.
- `migrate` applies the embedded migrations as the database owner, `seed`
  makes the demo organization, and `api` serves on `API_PORT` once both are
  done (#3). `worker` drains the outbox and prunes the audit log (#4). `web`
  serves the client on `WEB_PORT` once the api is healthy (#6).
