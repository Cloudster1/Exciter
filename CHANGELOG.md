# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- The interface and the API say "everyone", "all projects" or "Exciter"
  instead of "the organization" (#88). `GET /themes/active` answers `source:
  "everyone"` for the default theme, where it said `organization`.
- The Armature connection is its address (#87). Settings, Armature asks for
  no organization any more, and the webhook address is
  `/api/v1/armature/webhook`. Every member's token still has to come from the
  Armature the first token was made in.
- Letting somebody go switches their account off (#86). Members has Switch
  off and Switch on instead of Remove: switching off signs the person out
  everywhere and ends their tokens, and keeps their account, role and work.
  The installation always keeps an active administrator, owner or admin,
  rather than an owner. Everybody's tokens moved from `/api/v1/org/tokens` to
  `/api/v1/tokens/everyone`, and their page to `/settings/all-tokens`.
- Signing in lands in the installation (#85). The sign-in page asks for no
  organization, single sign-on starts at `/api/v1/auth/oidc/start` with the
  installation's provider, and there is nothing to switch between:
  `POST /auth/switch-org`, the organization switchers and the organizations
  on `/auth/me` are gone. `/auth/me` answers `installation` and `role`. The
  slugged `/auth/oidc/{orgSlug}/start` is removed rather than redirected, as
  no release has shipped with it.
- One installation is one team's requirements (#84). The first start makes
  the installation's one organization, called `EXCITER_NAME` (`Exciter` by
  default), which replaces `EXCITER_BOOTSTRAP_ORG_SLUG` and
  `EXCITER_BOOTSTRAP_ORG_NAME`; an administrator renames it under Settings,
  Installation. Somebody let go is told their account has been switched off,
  and their open session ends.
- Upgrading to one installation (#82, #89):
  - A database holding several organizations is refused, naming them; keep
    the one to go on with, delete the others with
    `DELETE FROM org WHERE slug = ...`, and migrate again.
  - `EXCITER_NAME` names the installation on its first start and replaces
    `EXCITER_BOOTSTRAP_ORG_SLUG` and `EXCITER_BOOTSTRAP_ORG_NAME`.
  - Removed without a redirect, as no release shipped with them:
    `/auth/oidc/{orgSlug}/start` (now `/auth/oidc/start`),
    `POST /auth/switch-org`, `/org/tokens` (now `/tokens/everyone`),
    `/settings/organization-tokens` (now `/settings/all-tokens`),
    `/armature/webhook/{orgSlug}` (now `/armature/webhook`) and the Armature
    connection's `orgSlug`.

### Added

- Requirements traced to Armature issues (#27). A requirement's editors trace
  it to the issues that implement, verify or relate to it, found by key or
  summary as they may see them in Armature; the requirement page shows each
  issue's summary, status and assignee live, and the status everyone may
  know without a token. The worker puts the requirement on each issue in
  Armature as the person who changed it, keeps it current through new
  revisions, keys, trash and purge, and says why when Armature refused it.
- Connecting Armature (#26). An administrator connects the installation's
  Armature under Settings, Armature, with the webhook's address, topics and
  secret; each person pastes their own Armature token under Your account,
  Armature account, sees whom it acts as, checks it and disconnects it, and
  is warned when it may only read. Tokens and the secret are sealed at rest,
  every call goes through a guard against reaching the server's own network,
  and a new address forgets every stored token. The stack runs an
  `armature-stub`, held to Armature's own document by a contract test.
- The API of the Armature integration, agreed before it is built (#25). The
  connection and each person's token, requirements traced to Armature issues,
  issues made from requirements, Armature's webhooks and the followed theme
  are in `api/openapi.json` and answer 501 until each is built.
  `docs/api-contract-m2.md` says what each operation means, how Exciter calls
  Armature as the viewer, and divides the migration numbers among the issues.
  Armature's own API is vendored under `api/armature/` with
  `make armature-openapi REF=`.
- Project templates (#24). A new project starts blank or from a System,
  Software or Test specification, which brings its requirement types, its
  attributes, the link types it uses and its starter sections; every
  template uses the default workflow.
- CSV import and export (#23). A project's administrators upload a CSV file
  under the project settings, see how each column will be read, and say
  what the words and people the project does not know mean before anything
  is written; the import then runs in the background with its progress
  shown, and refuses each row it cannot write with the line and why. A
  parent or outline column builds the tree and a links column holds
  `type:KEY`. The grid exports what it shows as CSV, and a project's export
  imports into an empty project as the same tree, content and links.
- Personal access tokens and MCP (#22). A person makes tokens under their
  account, read-only or not, limited to some projects or not, with an
  expiry, and sees each secret once; administrators see and revoke
  everybody's. A script sends one as a bearer and acts as its owner: a read-only token is refused every write, and a limited one
  reaches its projects alone, which the database holds too. `POST /mcp`
  serves assistants the tools to list projects, read, search, write, link
  and transition requirements and read their history, offering a token only
  what it may call. `GET /auth/me` names the calling token.
- Attachments (#21). A requirement takes files, picked or dropped on its
  panel, and its statement and rationale show pictures and files pasted,
  dropped or picked in the editor. Files go through the API alone, which
  shows pictures, PDFs and plain text in place and downloads the rest, and
  refuses one over `EXCITER_UPLOAD_LIMIT` (50 MB by default) with the limit.
  A file a revision shows is only taken off the list when deleted, so
  history still shows it; any other goes with its bytes, which the worker
  removes for purged requirements and deleted projects too. A copy takes
  the files and shows its own.
- Trace links (#18). A requirement links to another, in this project or any
  other the person may view, by the link types for all projects (derives,
  refines, satisfies, verifies, conflicts, relates) or a project's own. The
  links panel groups them by type and direction, finds the other end by key
  or title, and refuses a link to the requirement itself, a repeat, and a
  circle of a type that allows none, naming the requirements on the way
  round. A link records the revision of each end, is logged on both, and
  stays as a tombstone once removed. Administrators make and retire link
  types under Settings; a project turns one off or limits it to pairs of
  requirement types in its settings. A copy takes its links when asked.
- Search, the grid and saved views (#20). `/search` finds requirements across
  the projects a person may view by key, title, statement, rationale and
  text values, ignoring case and accents, with the matches marked in the
  title and a snippet, and narrows by project, status, label, owner and the
  day of the latest change. Ctrl K finds a requirement by the start of its
  key or title words. A project's grid narrows by words, kind, type, status,
  label, owner, priority and attribute conditions, orders by any column,
  shows the columns chosen and changes titles, priorities and values in
  place. A grid saves as a view, kept to its owner or shared with the
  project.
- Labels (#19). Any requirement or section carries
  labels shared by everyone, coined by typing one that nobody uses yet; the label box offers
  the labels already in use, the most used first, counting only what the
  reader may see. Adding or removing one is logged in the activity and never
  writes a revision. A label leads to the project's requirements with it,
  and on to those of every project. Copies take their original's labels, and
  the trash keeps them.
- Statuses and workflow (#17). Every project starts with a lifecycle: Draft,
  In review, Approved, Implemented and Verified, with Rejected and Obsolete
  beside them. A requirement moves only by the transitions its workflow
  offers the reader, and an approval cannot come from whoever wrote the
  latest revision. A new revision of an approved requirement sends it back to
  Draft, saying why. Project administrators define statuses, each counting as
  draft, review, approved, done or closed, and draw workflows in a graph
  designer, with conditions, validators and post-functions on each
  transition; the project follows one and a type may follow another, and a
  change of workflow asks where requirements go whose status it lacks.
- History, compare and restore (#16). A requirement's history lists its
  revisions with who wrote each, when and why. Any two compare side by side:
  changed words are marked in the statement and the rationale, and attribute
  values show before and after, options and people by name. Restoring an old
  revision writes it again as the next one, so nothing in between is lost.
  The activity merges revisions with owner, priority, place and trash changes.
- The tree and the specification (#15). Requirements and sections sit below
  one another, numbered as they read (2.3.1), and `/p/<key>/spec` reads a
  project as one numbered document beside its tree, each entry edited in
  place. Dragging in the tree moves a requirement and everything below it;
  the Move and Copy dialogs do it from the keyboard, also to another project,
  which gives new keys and keeps the old ones working. Types and values
  travel by name. The trash takes what is below a requirement along with it.
- Requirement types and attributes (#14). A project's administrators define
  its types, each with an icon and a colour, and the attributes it records:
  text, rich text, number, select, multi-select, date, person, yes or no and
  web address, each showing on some types or all of them and required or
  not. A requirement takes the project's default type unless its author picks
  another, and its values are part of what it says, so changing one writes a
  revision. What was ever used is retired rather than deleted, so history
  keeps it, and renaming an option renames it everywhere.
- Requirements (#13). An editor writes a requirement or a section with a
  title, a statement and a rationale in the shared editor, and gives it an
  owner and a priority. Its key is the project's key and its number, such as
  `CHK-R42`, and numbers are never handed out twice. A save that changes what
  it says writes the next revision, made over the revision it was opened at;
  a save over a revision somebody else saved first is refused and keeps the
  draft to save again over theirs. Owner and priority changes are logged
  without a revision. Deleted requirements wait in the project's trash until
  an editor restores them or an administrator deletes them for good. Old
  keys, after a project's key changes, still find their requirements, and
  `/r/<id>` is a permalink that never breaks. A project now opens on its
  requirements.
- Project roles (#12). A project's administrators give people, groups and
  everyone the role viewer, reviewer, editor or admin under
  `/p/<key>/permissions`, and roles add up: an editor through a group who is
  a reviewer on their own both edits and approves. A new project is its
  maker's to run and everyone's to edit. Guests are let in only by grants
  that name them or their groups, and never run a project. The access
  inspector shows, for any member, what they may do in the project and which
  grant decides it, as the database answers it. The installation's owners and
  administrators still hold every right in every project.
- Projects (#11). Owners and administrators make a
  project with a key such as `CHK`, which the form suggests from the name and
  the server keeps free. A project can be renamed, given a new key, archived
  and deleted once archived. A key it had keeps finding it, and stays taken.
  `/projects` lists them, `/p/<key>/settings` holds a project's details, key,
  archive and deletion, and the sidebar names the project the reader is in and
  switches to another.
- The API of the requirements core, agreed before it is built (#10). Projects,
  permissions, requirements and their revisions, types and attributes, the
  tree, history, workflow, trace links, labels, search, attachments, tokens,
  CSV import and templates are in `api/openapi.json` and answer 501 until each
  is built. `docs/api-contract-m1.md` says what each operation means and
  divides the migration numbers among the issues.
- A browser suite with an accessibility check on every page (#9).
  `make test-e2e` runs Playwright in its own container against the compose
  stack, and joins `./run-tests.sh` and CI, which splits it in two shards and
  merges their reports into one. axe checks every page of the shell, a new
  theme's editor, the not-found page and the sign-in page in light and dark,
  at desktop and phone widths. The specs sign in with a password and out
  again, sign in through the stack's Keycloak as alice@suite.test and land in
  the installation with the role her groups give, switch light and dark from
  the rail, import Armature's Minecraft theme, name the default theme for
  everyone and see it as another member, add, re-role and switch members off
  and on, and page through the audit log by mouse and keyboard. Each spec file
  keeps to people and project keys of its own, and the specs that change
  what everyone sees run one at a time (#83). `make test-e2e ONLY=<grep>` runs part of it. The link
  to the themes on the default theme page is underlined, which axe asked for.
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
- Exciter has a logo and a favicon (#56). The sign-in page shows the logo, the
  sidebar shows the gopher's face beside the installation's name, and the
  browser tab, touch icon and web app manifest use the same face, which still
  reads at 16 pixels. `make branding` makes them from
  `assets/branding/logo.jpg`.
- Single sign-on through the installation's identity provider (#7). The
  sign-in page sends the reader to the provider (authorization code with
  PKCE); a person is matched by the
  provider's subject, so they are the same user in Exciter, Armature and
  Stator, and their provider groups are synced at every sign-in.
  Administrators set the provider up under Settings, Single sign-on, map
  provider groups to the admin or member role, which members follow at their
  next sign-in, and let in or turn away people the provider vouched for who
  are not members yet; every change is in the audit log.
  `make up` signs the demo installation in through the stack's Keycloak as
  alice@suite.test (an administrator) or bob@suite.test, and behind nginx
  sessions and the audit log record the client's address rather than the
  proxy's (`EXCITER_TRUSTED_PROXIES`).
- The web client's skeleton (#6). nginx serves the React app on the web port
  and passes `/api` to the api, so `make up` now opens Exciter at
  `http://localhost:<web port>`. A person signs in with a password, lands on a
  home page that says what Exciter is for, and finds their way by a rail, a
  sidebar that folds or becomes a drawer on a phone, breadcrumbs, and Ctrl K,
  which goes to any page from the keyboard. The account menu switches between
  English and German and signs out. Administrators add members (a made
  password is shown once), change their roles, switch them off after a
  confirmation, and read, filter and export the audit log. A
  session that ends sends the reader to sign in and back. `make check-web`
  and `make check-format` join the gate.
- Members and password sessions (#5). `POST /auth/login` signs a local
  account in with an argon2id password and sets an http-only session cookie;
  `/auth/me` and `/auth/logout` follow it. An administrator lists, adds,
  re-roles (admin, member, guest) and switches off members, each act in the audit log, which administrators now read, filter
  and export through `/audit`. `seed bootstrap` makes the installation and its
  owner; `make up` signs in as `admin@exciter.test`.
- The outbox, the worker and the audit log (#4). A change and the event it
  causes commit together or not at all; the worker drains the outbox, any
  number of copies at once, retries a failing event a little later each time
  and after ten attempts sets it aside with an entry in the
  audit log. The log is only ever added to, read only by owners and
  administrators, and pruned after `EXCITER_RETAIN_AUDIT` (a year by default,
  never less than a day). Its endpoints are agreed and served with #5.
- The backend skeleton (#3). The api serves `/healthz`, `/readyz`,
  `/api/v1/openapi.json` and `/api/v1/meta` from one route table that also
  writes `api/openapi.json`; reads go to the replica unless it has not
  replayed the caller's own last write. `make up` now migrates, seeds the demo
  installation and starts the api. Every table forces row level security, and
  the integration suite proves through raw SQL as `exciter_app` that the
  permission policies hold.
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
