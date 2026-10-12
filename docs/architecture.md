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
  milestone's contract divides a range of migration numbers among its issues.
- Configuration: environment variables only, prefix `EXCITER_`.
- Observability: `log/slog`, Prometheus metrics, OpenTelemetry traces.

### Multi-tenancy

Every table has `org_id` and a primary key `(org_id, id)` with composite
foreign keys, and FORCE ROW LEVEL SECURITY on `current_org_id()`. The
application connects as `exciter_app`, which row level security binds;
permission rules are SQL functions used as restrictive policies, so a guard
holds even for a query that forgot it.

### Domains (M1 to M3)

- **Projects** (#11): a key such as `CHK`, settings, templates (#24).
- **Requirements** (#13): key `CHK-R42`, a rich-text statement, rationale,
  type and attributes (#14), a place in the tree (#15), a status from the
  project's workflow (#17).
- **Revisions** (#13, #16): a save that changes the normative content writes
  an immutable revision; status, owner, labels and position are logged but
  never revisioned. Reviews, baselines and links point at revision numbers.
- **Trace links** (#18): typed and directed links between requirements; a
  link turns suspect when its upstream end has a newer revision (#36).
- **Review and approval** (#35), **baselines** (#37), **coverage** (#38).

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
`docs/design/01-tokens.md`, which follow `prefers-color-scheme` until themes
arrive in Armature's `armature-theme/1` format (#8). Strings live in
`web/src/i18n`, English and German.

- `api/client.ts` is the one place that calls the API, typed by
  `api/schema.d.ts`, which `make web-schema` generates from
  `api/openapi.json`. A 401 from any request sends the reader to `/login` and
  back afterwards.
- Routes are defined in code (`routes/index.ts`); everything but `/login`
  sits in the shell (`features/shell`): a rail, a sidebar that becomes a
  drawer below the `shell` breakpoint, a top bar with breadcrumbs, the
  account and organization menus, and the Ctrl K quick switcher. The places
  of a build are listed once, in `features/shell/places.ts`.
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
- `migrate` applies the embedded migrations as the database owner, `seed`
  makes the demo organization, and `api` serves on `API_PORT` once both are
  done (#3). `worker` drains the outbox and prunes the audit log (#4). `web`
  serves the client on `WEB_PORT` once the api is healthy (#6).
