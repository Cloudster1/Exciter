# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Themes (#8), in Armature's `armature-theme/1` format, so a theme exported
  from Armature or Stator imports unchanged and leaves the same way. The rail
  switches between Auto, Light and Dark; under the account menu, Themes lists
  a person's own themes and those shared with them, starts a new one blank or
  from Deep-Tech or Constellation, and edits its colours (the Clay tokens of
  Armature's `docs/design/01-tokens.md`), type, corners, shadows, cursors,
  icons, backdrop, moving picture and extra CSS with a live preview. A theme's
  pictures and fonts are kept in the object store, which the api now reaches
  through `EXCITER_S3_ENDPOINT` and its keys, making its bucket when missing.
  Administrators name the shared theme everybody sees until they choose under
  Settings, Default theme, recorded in the audit log. The database keeps a
  private theme its owner's, lets only the owner or, once shared, an
  administrator change one, and keeps each choice to the person who made it.
- Single sign-on through the organization's identity provider (#7). The
  sign-in page takes the organization's name and sends the reader to its
  provider (authorization code with PKCE); a person is matched by the
  provider's subject, so they are the same user in Exciter, Armature and
  Stator, and their provider groups are synced at every sign-in.
  Administrators set the provider up under Settings, Single sign-on, map
  provider groups to the admin or member role, which members follow at their
  next sign-in, and let in or turn away people the provider vouched for who
  are not members yet; every change is in the audit log. A sign-in through a
  provider reaches the organizations that trust the same provider.
  `make up` signs the demo organization in through the stack's Keycloak as
  alice@suite.test (an administrator) or bob@suite.test, and behind nginx
  sessions and the audit log record the client's address rather than the
  proxy's (`EXCITER_TRUSTED_PROXIES`).
- The web client's skeleton (#6). nginx serves the React app on the web port
  and passes `/api` to the api, so `make up` now opens Exciter at
  `http://localhost:<web port>`. A person signs in with a password, lands on a
  home page that says what Exciter is for, and finds their way by a rail, a
  sidebar that folds or becomes a drawer on a phone, breadcrumbs, and Ctrl K,
  which goes to any page or organization from the keyboard. The account menu
  switches between English and German and signs out; a person in more than
  one organization switches between them in the top bar. Administrators
  add members (a made password is shown once), change their roles, remove
  them after a confirmation, and read, filter and export the audit log. A
  session that ends sends the reader to sign in and back. `make check-web`
  and `make check-format` join the gate.
- Organizations, members and password sessions (#5). `POST /auth/login` signs
  a local account in with an argon2id password and sets an http-only session
  cookie; `/auth/me`, `/auth/switch-org` and `/auth/logout` follow it. An
  administrator lists, adds, re-roles (admin, member, guest) and removes
  members, each act in the audit log, which administrators now read, filter
  and export through `/audit`. `seed bootstrap` makes the first organization
  and its owner; `make up` signs in as `admin@exciter.test`.
- The outbox, the worker and the audit log (#4). A change and the event it
  causes commit together or not at all; the worker drains the outbox, any
  number of copies at once, retries a failing event a little later each time
  and after ten attempts sets it aside with an entry in the organization's
  audit log. The log is only ever added to, read only by owners and
  administrators, and pruned after `EXCITER_RETAIN_AUDIT` (a year by default,
  never less than a day). Its endpoints are agreed and served with #5.
- The backend skeleton (#3). The api serves `/healthz`, `/readyz`,
  `/api/v1/openapi.json` and `/api/v1/meta` from one route table that also
  writes `api/openapi.json`; reads go to the replica unless it has not
  replayed the caller's own last write. `make up` now migrates, seeds the demo
  organization and starts the api. Every table forces row level security, and
  the integration suite proves through raw SQL as `exciter_app` that one
  organization cannot read another's rows.
- A development stack (#2). `make up` starts Postgres with a streaming
  replica, Valkey, SeaweedFS, Mailpit and Keycloak on twenty ports of the
  checkout's own between 10000 and 19999, so worktrees and a Stator checkout
  run beside it; `make ports` says which. Keycloak's `suite-dev` realm has a
  client for Exciter, Armature and Stator, so a person is one subject in all
  three.
- Repository tooling (#1). `./run-tests.sh` is the one gate for every layer,
  run before each push by the branch-guard hook and in CI as a matrix of jobs;
  `release.sh` cuts a version from the commit history; issue and pull request
  templates and the GitHub workflow live in `.claude/skills/gh`.
