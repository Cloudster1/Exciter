# Decisions

Newest first. Each entry says what was decided and why, so a later change can
tell whether the reason still holds.

## 2026-10-16: Webhook envelope version 2 is a CloudEvent beside version 1 (#133)

- **A second row, not a new sender.** Version 2 is one more entry in the
  sender's table of envelopes, so retries, redeliveries, signatures, the
  owner's rights and the log are version 1's; only the body and its content
  type differ. `webhook_endpoint.version` already held any version, so no
  migration was needed.
- **The data is version 1's payload unchanged.** A receiver moving to
  version 2 rewrites its envelope handling, never its payload handling, and
  `dataversion` versions the payload apart from the envelope.
- **`source` is the installation's address.** CloudEvents makes source and
  id the event's identity; a fixed `/exciter` would make two installations'
  events indistinguishable to a relay that hears both.
- **Signatures stay in headers.** CloudEvents leaves authentication to the
  transport, and keeping version 1's headers lets one receiver check both
  versions with the same code.
- **Exciter reads version 2 before the partners send it.** The shape is the
  one Armature #286 and Stator #434 agreed, so the receivers are ready when
  either ships, and the stubs post it for the tests. One decoder serves both
  receivers, and a header and a body that disagree are refused like an
  unknown version, since guessing would act on what the sender did not mean.
- **The shared list of event types is still to come.** Dynamo#4 is not
  written; until it is, the types are in the API document, with every
  payload.

## 2026-10-16: Themes are proven to travel with the partners' own files (#59)

- **Real exports, not examples.** A theme was made with a file in a running
  Armature and in a running Stator, and their exports are vendored byte for
  byte with the build they came from. Examples written by hand could carry
  what no export does, such as a trailing newline in the CSS, which all
  three trim as they save; the comparison trims it the same way.
- **The round trip compares what a theme says.** An import gives files new
  ids, so files are compared by order, name, type and bytes, and the spec
  with each id replaced by its file's place.
- **Exciter's file is a golden export.** It is made through the API from a
  fixed theme and compared with the committed copy on every run; rewriting
  it is a switch, so a change to the export is a decision, after which
  Armature and Stator vendor it again (Cloudster1/Armature#295,
  praetorianer777/Stator#443).
- **No tokens of Exciter's own yet.** All three name the same 44 tokens. A
  token a later version adds travels through the others because each keeps
  any well-formed name and draws only what its stylesheet has; the tests
  prove that path now, so the first token one product adds does not need
  the others to move first.
- **The lists are vendored, not fetched.** A test that fetched the partners'
  files would fail whenever they change and need the network; vendoring with
  the commit makes following them a deliberate step, as `make
  stator-openapi` is.

## 2026-10-16: Reuse is a locked copy that follows its library source (#49)

- **A copy, not a pointer.** A reuse keeps its own words, written by a take,
  so search, baselines, export, coverage, reviews and webhooks read it as any
  requirement; a live reference would have each of them read through it.
  What makes it "written once" is the lock: while it follows, only a take
  changes its words, and the database holds it to that.
- **Only libraries are sources.** A project's administrators mark it, and
  marking is audited, so reuse is deliberate and a reader knows where the
  shared requirements live.
- **Words follow, the rest is the target's.** Title, statement and rationale
  follow; type, values, status, owner, priority, labels, links and reviews
  stay the target's, as they mean something in that project only.
- **The guard compares words.** A take copies the files its words show, with
  ids of their own, so the documents differ by those ids while what they say
  is the source's. The guard compares the title and the plain text of the
  statement and the rationale; the service, stricter, refuses any change to
  the documents of a following copy.
- **A variant still sees its source.** It keeps the revision it branched
  from, so a newer source revision shows as behind, and a take has it follow
  again; there is no way to forget the source short of purging it.
- **Watchers hear of the source,** about the reuse they watch, and only if
  they may read the source, so nobody learns of a library they cannot open.
- **No MCP tool yet.** The tools are an agreed list; reuse joins it with its
  own agreement.

## 2026-10-16: Stator's requirement blocks read what Exciter already serves (#43, #140)

#43 is Stator's to build. Exciter's part is to name what the chips and blocks
read, agree how, and file the rest in Stator.

- **A list is a capability of its own.** `requirement-list@1` promises the
  grid's query read and its completion, so `requirement-embed@1` keeps the
  promise a partner may already have checked; a partner that wants both asks
  for both.
- **Webhooks under Stator's name.** Exciter's outbound webhooks are Stator's
  with the same eight operations, so they are listed as `webhooks@1`, which
  lets Stator know before it asks an administrator for one.
- **JSON only.** Stator draws chips and blocks from the API as the viewer, as
  it does Armature's issues; no oEmbed and no page made for framing, which
  would need their own rules on who sees what.
- **Webhooks only clear Stator's cache.** A payload is read as the webhook's
  owner, so Stator re-reads with the viewer's token rather than showing it.

## 2026-10-16: Requirements show on the Stator pages they trace to, as on Armature's issues (#42)

Stator shipped remote links on pages (Stator#419, its pull request #437), so
the way back of `docs/api-contract-m4.md` is built as Armature's link sync of
#27, pointed at pages.

- **The capability is Stator's spelling.** Stator names every capability
  `name@version` with a test that forbids a dot, so it lists
  `page-remote-links@1` rather than the `page.remote-links` the contract
  asked for. `RemoteLinksCapability` follows it, and nothing else changes.
- **The catch-up is each link's maker's.** Once Stator first lists the
  capability, the connection's trigger queues every requirement with live
  page links as the maker of its newest one, so a page shows only what
  somebody who picked it would have put there, and nobody's token reaches a
  page they never chose. A requirement whose live links all lost their maker
  waits for its next change.
- **Nothing is queued while Stator cannot take it.** `stator_links_emit`
  writes no event while the connection's `remote_links` is false, and the
  links read `sync` `none` then, rather than `pending` forever.
- **A failure waits for the next change,** as on Armature: the sentence says
  so, and no button tries again, since the next try would meet the same
  refusal until the person or the page changes.
- **A page Stator deleted forgets its records.** Its remote links go to the
  trash with it, so the sync has nothing left to take off there.
- **The stub offers what Stator offers.** Its `/meta` lists Stator's own
  capabilities unless a test sets others, and `keepStator` puts them back,
  as the Armature stub does.

## 2026-10-15: Webhooks are Stator's, kept by the installation and filtered by project (#48)

Integrators want to hear when requirements change, signed so they know it came
from this server; Stator and Armature already send theirs the same way.

- **Stator's sender, ported.** The endpoint, the log of attempts, the sealed
  secret shown once, the owner whose rights each payload is read with, the
  leased sender, the backoff and turning a failing endpoint off come across
  as they are, so a receiver written for Stator or Armature serves Exciter.
- **Kept by the installation, with a project filter.** An integration usually
  feeds one or two projects' requirements to a test or delivery tool, so an
  endpoint may name its projects; one naming none hears every project its
  owner may see, as Stator's hears every space.
- **References, read as the owner.** A payload names the requirement, the
  actor and what changed, read when the attempt is sent; a receiver fetches
  more through the API. A body of the whole requirement would say more than a
  receiver usually needs, and go stale between attempts.
- **Every topic the issue named, none more.** Edits of comments, reopened
  threads and imports stay out until somebody asks; the database's list of
  topics and the outbox's are held together by a test.
- **Its tests change the installation.** An endpoint hears every project, so
  a test that makes one puts the installation's endpoints back through the
  keeper `keepWebhooks` and is listed in `installationWide`.

## 2026-10-15: The query language is Armature's, compiled per project (#47)

A power user narrows the grid in one line rather than a row of controls, and
someone who knows Armature's query language should not learn another.

- **Armature's language, ported.** The lexer, parser and completion of
  `internal/nql` come across as they are, so the two products read a query
  the same way; Exciter adds limits on length, depth and conditions, which an
  open api needs.
- **A catalog per project.** Armature's catalog is fixed and finds custom
  fields by name across projects in SQL. The grid is one project's, so the
  catalog is built from its attributes and link types: a misspelt attribute
  or an option it lacks is a refusal pointing at the character, not a query
  that quietly finds nothing.
- **Links read from either end, by their phrases.** `verifiedBy IS EMPTY` is
  the question traceability asks; a field per end named after the type's
  phrase says it as the requirement page does. A phrase that would clash
  falls back to the type's key.
- **Coverage judged once more, in SQL.** The coverage report judges in Go
  from counts. A query needs the level in a WHERE clause, so
  `requirement_coverage` judges the same way; a test holds the two to the
  same answers rather than one replacing the other in this change.
- **Beside the controls, not instead of them.** The grid's dropdowns,
  attribute conditions and suspect chip stay and narrow with the query; the
  words box switches to the query box, since both are a line of text.
- **Completion as in Armature, one fix.** A suggestion is highlighted by a
  pointer that moves, not by one the list opened under, which in Armature
  lets Enter pick a stale row instead of running the query.

## 2026-10-15: An approval can be an electronic signature, proven again every time (#46)

Some projects need their approvals to stand as signatures: the record has to
show who signed, what it meant, what exactly was signed, and that it was the
signer and not whoever sat at their screen.

- **A project setting, off by default.** Most projects review without it, and
  a signature costs the reviewer a password each time; the administrators who
  need it turn it on, and the change is audited.
- **Approve and reject are both signed**, since a rejection is as much a
  decision on the record; asking and withdrawing are not decisions.
- **The meaning is a sentence the server writes**, one per decision, naming
  the revision, the requirement and the signer's part. A meaning typed or
  picked by the signer would make signatures hard to compare, and the server
  knows what the decision does.
- **Every signature proves who one is again.** Nothing carries over, not even
  from the signature a minute ago, so an unattended screen signs nothing. A
  password is checked as Armature's password change checks one, with a limit
  of five wrong ones in fifteen minutes. Somebody without a password signs in
  again at the identity provider with `prompt=login` and `max_age=0`, and the
  callback checks `auth_time`, the person and the session before it records a
  proof good for one signature and five minutes.
- **The database holds the line.** A signature spends its proof in the same
  statement, must name the revision's content hash, and a decision in a
  signing project without a signature from the same transaction is refused,
  so neither a bug in the service nor a raw write makes an unsigned approval.
- **Tokens cannot sign.** A token proves an automation, not a person present.

## 2026-10-15: A document is printed by the render service and written as Word by the api, as in Stator (#45)

A specification or a baseline is exported to be signed and sent, so it has to
read as the project reads, in both formats people send.

- **PDF is the print view printed as its reader.** Stator's render service, a
  headless Chromium behind `render/server.mjs`, opens the web client's
  `/print/...` view with a read-only token made for that one print and
  deleted once the PDF is back, the check `api_token_render_is_brief` holding
  a raw write to the same. Rich text, tables and pictures print as the editor
  draws them, which HTML made on the server would only imitate. A token held
  to some projects cannot print, since the print's token reads them all.
- **The print reads what its reader just wrote.** The print's requests go
  through the replica as its own token, so the token's write position is
  noted under its key: a baseline taken and printed at once is found.
- **Word is written in the api.** Stator's `internal/docx`, the writing half,
  cut to a requirement's blocks; no Go library writes Word well without a
  commercial licence, and the render service makes no Word documents.
- **One document, two formats.** `internal/specdoc` reads the cover, the
  entries, their tables of fields and attributes, and their links once; the
  print view draws it and the Word writer writes it, so the two say the same.
  A baseline's document holds each requirement at the revision it holds, and
  no owner, priority or labels, which a baseline does not keep.
- **The renderer is in the stack and optional in the chart.** The compose
  stack runs it, so the integration and browser suites print real PDFs. The
  chart, which left it out (#104), now has it behind `render.enabled`, off by
  default for its size; without it only a PDF download fails, in a sentence.
- **Exports are audited** as `specification.exported`, since a document that
  leaves to be signed is worth knowing about.

## 2026-10-15: A ReqIF exchange keeps the identifiers it met (#44)

ReqIF is exchanged both ways: a specification goes to a supplier and comes
back changed. An import that matched by name alone would make everything
again, or match the wrong thing.

- **Identifiers have a table of their own.** `reqif_identity` ties each
  identifier of a file to the requirement, type, attribute on a type, option,
  link type or section it became, and an export writes those identifiers
  back. What no exchange named is written as `exciter-` and its id, which an
  import into the same project resolves without a tie. A column on each table
  would have needed one per table, and a requirement can come from several
  exchanges.
- **What matches nothing is made, and shown first.** A supplier's types and
  attributes rarely match a project's by name, and refusing them would make
  every first exchange a mapping chore. The preview lists what will be made,
  and each item can be mapped or left out before anything is written.
- **Made things belong to the project.** Link types an import makes are the
  project's own, never the installation's, so one project's exchange changes
  no other project.
- **An exchange adds, it does not remove.** Links are added and never taken
  away, and requirements the file does not hold stay as they are; a status
  is set only on what the import makes. A file is one party's view, and a
  missing object is more often out of its scope than deleted.
- **Files wait in the database.** The files an archive's text shows are kept
  with the import, not in the bucket, until the worker makes them
  attachments, so the API and the worker need share nothing but the
  database. A requirement the import makes shows them from revision 2, since
  an attachment needs its requirement first.

## 2026-10-15: The development realm is the suite's `dynamo`, the same file in every tool (#102)

Stator, Armature and Exciter sign in through one Keycloak realm, so one
sign-in works in all three and Dynamo starts one Keycloak for its tests.

- **One file.** `deploy/keycloak/realm.json` is byte for byte Armature's,
  which Armature built from Exciter's `suite-dev`: clients `armature`,
  `exciter` and `stator` with the secrets `<id>-dev-secret`, the `groups`
  mapper, and alice, bob, carol and dave. Exciter uses only its own client;
  the others are there so the same file serves the suite. The secrets are for
  development only; Keycloak refuses unknown top-level fields, so the README
  says so rather than the file.
- **The partners' groups come along.** alice is also in
  `armature-administrators` and `stator-administrators`, bob in `developers`,
  carol in `portal-owners` and `scrum-masters`, dave in `readers`. With
  `createGroups` Exciter makes those groups on sign-in; none is mapped to a
  role, so they change nobody's rights.
- **Dynamo's copy is canonical.** `make realm-check`, Armature's script, runs
  in the shell layer and compares the file with Dynamo's after sorting keys,
  failing with the first line that differs. While Dynamo holds no realm it
  says so and passes. It needs the network, as Armature's does; a check that
  passed offline would say nothing.
- **The stack test ties the bootstrap to the realm.** `tests/test-stack.sh`
  fails when compose's issuer, client or secret is not what the realm holds,
  so a rename cannot leave the seed pointing at a realm that is gone.

## 2026-10-15: Exciter installs with Stator's chart, and the bootstrap connects the partners (#104)

Dynamo's umbrella chart depends on each tool's chart, so the three install
alike.

- **Stator's chart, renamed.** `deploy/charts/exciter` keeps its value names
  wherever the meaning is the same: the database layouts (CloudNativePG, one's
  own, or bundled for a trial), Valkey, the S3 store with SeaweedFS for a
  trial, the secrets Job, the roles, migrate, bootstrap and seed Jobs and
  their hook order under Helm and Argo CD. What Exciter does not have, the PDF
  renderer, backups and the page and space settings, is left out.
- **Partners are values.** `armature.url` and `stator.url` are the addresses
  people open. The bootstrap Job connects each where nothing is connected, so
  an umbrella chart wires the suite with one value per partner;
  `ConnectFirst` saves the address before the partner answers, since the
  three install at once, and what it offers is asked on the next save or
  token check. An address an administrator set is never overwritten.
  Webhook secrets stay a step in Settings: a partner makes one only when its
  webhook is added.
- **A backchannel lets its host through.** Every host
  `armature.backchannel`, `stator.backchannel` or `auth.oidcBackchannel`
  reaches is added to `EXCITER_OUTBOUND_ALLOW`, since a backchannel the guard
  refuses is never what was meant.
- **Sign-in through the suite's realm is bootstrap values.** The issuer, the
  client id and a Secret holding the client secret, as Stator's chart takes
  them; nothing here names the realm `dynamo` (#102).
- **Two kinds of test.** `tests/test-helm.sh` renders every layout and checks
  the values that matter by name, as Stator does, and compares two whole
  renders with golden files, as Armature does, so an unintended change to
  any manifest shows in review. `make test-chart-install` installs and
  upgrades the chart in kind, in CI when what the chart installs changes.
  There is no previous release to upgrade from yet; the upgrade is of the
  same chart with a second api pod, as Stator's is.

## 2026-10-15: A release is a pull request, and its merge commit is the tag (#101)

Exciter releases as Stator does since its #410, and Armature since its #240:
`./release.sh` infers the next semver from the Conventional Commits and writes
the notes, and a `v*` tag publishes. Main takes no direct commits, so the bump
travels as a pull request of its own.

- **The bump travels on `release/vX.Y.Z`.** `release.sh` keeps its checks (on
  `main`, not behind `origin/main`) and adds a clean tree and an unused tag
  and branch, then commits the version files and the changelog on that
  branch, pushes it and opens its pull request. The branch guard accepts the
  name without an issue: the version is its identity and the pull request its
  record.
- **The workflow tags the merge commit, not the script.** A tag set by
  `release.sh` would sit on a commit main does not have, and one set by hand
  after the merge is a step that gets forgotten. `scripts/release-tag.sh`
  refuses a commit main lacks, version files that say another version and a
  tag that points elsewhere; the message is the release's `CHANGELOG.md`
  section, which the pull request showed for review.
- **One run does the rest.** A tag pushed with the workflow's own token starts
  no workflow, so the image and release jobs follow the tag job in the same
  run; a tag pushed by hand and `workflow_dispatch` start them too.
- **One list of version files.** `scripts/version-files.sh` names them for
  `release.sh`, the workflow and the test that refuses them disagreeing:
  `VERSION`, the chart's `version` and `appVersion` once #104 adds the chart,
  and a `package.json` once it carries a version.
- **Images are `ghcr.io/cloudster1/exciter-<image>`**, `backend` and `web`,
  tagged `X.Y.Z` and with the short commit. A test keeps the list equal to the
  images the stack builds and the chart runs; the chart is
  `oci://ghcr.io/cloudster1/charts/exciter` since #104.
- **Dynamo is told, not waited for.** A `repository_dispatch`
  (`tool-released`, `{tool, version}`) goes to Dynamo when
  `DYNAMO_DISPATCH_TOKEN` is set; without it the release still stands and the
  run leaves a notice.
- **A workflow that runs only on a release is checked on every push:**
  actionlint over `.github/workflows` and the image names, in
  `tests/test-release-workflow.sh`.

## 2026-10-15: Webhook envelopes are read by their version (#103)

The three tools post webhooks in one envelope that names its version, so its
format can change without a receiver misreading it.

- **The header decides before the body is read.** A signed delivery whose
  `X-Armature-Webhook-Version` or `X-Stator-Webhook-Version` names a version
  Exciter does not read is refused with 422 `unknown_version` and a warning,
  and so is one whose body does. The signature is checked first, so an
  unsigned caller learns nothing from the answer.
- **No version is version 1.** Stator sends none until its #412, and
  Armature sent none before its #242; refusing them would cut off a partner
  that has not upgraded.
- **The envelope Exciter reads is in its own document.** The receivers name
  their bodies `ArmatureWebhookEnvelope` and `StatorWebhookEnvelope` instead
  of an untyped value, and a unit test compares each with the sender's
  vendored schema, so a field leaving the sender fails here first.
- **One version for the suite.** `internal/webhook` holds the version Exciter
  reads, which is also the one it will send under `X-Exciter-Webhook-Version`
  once it sends webhooks; nothing sends them yet, so no sender is built ahead
  of a use.

## 2026-10-15: Capabilities, offered and checked by exact name (#100)

Exciter lists what it offers partners under `capabilities` of `/meta`, and
checks what Armature lists before each feature calls it.

- **Names follow Armature's until Dynamo's list exists.** Exciter offers
  `requirement-embed@1`, `trace-links@1`, `specifications@1` and
  `theme-export@1`, the last by Armature's name because the file format is
  theirs. Dynamo#4 is to hold the shared list; these move to it once it does.
- **Each name rests on routes the build has.** As in Armature, the list names
  its operations, and a unit test refuses one the table lacks or has only
  agreed, so a renamed route fails here rather than at a partner.
- **A partner's capability counts by its exact name.** `remote-links@2` does
  not stand in for the `@1` a feature was built against, which is what a
  raised number means. Stator's `page-remote-links` is matched by its bare
  name under any version, as #42 agreed before Stator named it.
- **Not asked counts as offering everything.** A connection saved before this
  release has no list until it is saved again or a token checked, so
  upgrading Exciter alone switches nothing off before Armature is asked.
- **Each feature says so where it is used.** Reads answer `outdated`, writes
  are refused with `armature_outdated`, the link worker records the sentence on
  the link, and the settings and account pages list every missing name with
  what goes without it and to update Armature.

## 2026-10-15: Every build says what it is at GET /api/v1/meta (#99)

Stator, Armature and Exciter answer the same unauthenticated `/meta`, so a
partner, an operator or Dynamo can tell which build of which tool it talks to.

- **The answer is the suite's, not Exciter's.** `product` is `exciter` in
  lower case, as the other two name themselves; the OpenAPI document keeps the
  title `Exciter`. `apiVersion` is the document's `info.version`, which rises
  only for a change that breaks clients, and `capabilities` stays an empty
  list until #100 names them.
- **The stamp comes from the checkout, not the image.** `scripts/build-stamp.sh`
  is Stator's: `1.2.3` on the tag `v1.2.3`, `1.2.3+4.gabc1234` past it, and
  `VERSION+dev` with no tag in reach, with the short commit. The Makefile runs
  it once and compose passes both to the backend image, because the build
  context has no `.git` and a worktree has one only as a file.
- **Anybody may read the commit.** Armature shows it only to somebody signed
  in; Exciter answered it to anybody before, as Stator does, and keeps that.

## 2026-10-15: The Stator integration, agreed before it is built (#39)

`docs/api-contract-m4.md` settles #40 to #42 as M2 settled Armature's, and
says what differs.

- **A page link watches the page.** A page specifies or describes a
  requirement, so the requirement answers to it: the link turns suspect when
  the page publishes a newer version than the one last confirmed, never when
  the requirement is revised. For that the page's latest version is stored on
  the link for everyone, as an issue's status category is, and nothing else of
  the page but its id, space key and URL.
- **Pages count for nothing in coverage.** A page explains a requirement; it
  neither decomposes, implements nor verifies it.
- **The way back waits for Stator, and asks it.** Stator has no remote links
  yet. Exciter asks for them in one Stator issue, and puts its links on pages
  only once Stator lists `page-remote-links` (first asked as
  `page.remote-links`) among its capabilities on
  `/meta`, so neither side has to ship in step with the other.
- **Stator's own webhooks.** Stator already sends signed page events to any
  address its administrator names; Exciter receives them like Armature's
  rather than asking for a dedicated channel.

## 2026-10-14: Review and control, agreed before it is built (#32)

`docs/api-contract-m3.md` settles #33 to #38 before any of them is built, as
M1 and M2 were.

- **A review signs off a revision that is.** Stator's page review approves a
  draft and publishes it; an Exciter revision is saved already, so a review
  pins the current one, an approval counts only while it is still current,
  and a new revision supersedes what was pending. Approval rules borrow
  Stator's shape: people or groups, a count, per type.
- **Suspect is computed, never stored.** Links already remember the revision
  of each end they were confirmed against, and a type already says which end
  it watches (#18); a view compares, and clearing moves the remembered
  revisions on with a record of who, against what and why.
- **Baselines copy nothing.** An item names a revision, taken in one
  REPEATABLE READ snapshot, and never changes; a requirement's diff is #16's.
- **Coverage counts the status stored for everyone** (#27), so it never asks
  Armature as the viewer.
- **The migration ranges moved up by a hundred**, to 00500 and on, because
  the issues' 00400 to 00470 went to the one-installation work and M2.

## 2026-10-14: A removal Armature announces is a person's only for a wanted link (#29)

"Armature removal wins" (#1) needs to know who removed Exciter's link from an
issue, and Armature announces every removal alike, the worker's own included.
The worker takes a link off only when Exciter no longer wants it there: the
trace was removed, the requirement went to the trash or was purged, or the
link is broken or detached. So `issue.remote_link.removed` detaches a link
only while its requirement is live, the link neither broken nor detached,
and the sync record names the removed link's id. The worker's removal can be
announced before its own transaction forgets the record; the requirement's
state, committed before the worker ran, still tells the two apart.

## 2026-10-14: The tables are the installation's (#108)

The last of #91's three steps, as Armature's #218 took it: what was left of
the organization was names. `org` is `installation`, with the name and the
default theme and its id kept; `org_join_request` is `join_request`; a
membership is a role on the account; the identity provider and the Armature
connection are one row each; no table carries `org_id`, and
`current_org_id()` and `installation_org_id()` are gone.

- **A role on `app_user`, not a table.** `role`, `role_source` and
  `joined_at`, NULL for somebody who asked to join. Every rule of
  `org_member` holds on the columns: only an administrator gives or changes
  a role, never an owner, through the app role; the provider moves no
  owner; the last active administrator stays. What the keys to the
  membership did is a trigger now: a token, a grant or a requirement's owner
  names a member (`held_by_a_member`), and taking a role away, which only
  the admin role may, takes those along (`app_user_left`).
- **Deleting an account is no longer the app role's.** Any member could
  delete anybody's account straight through SQL, membership and all; the
  app role lost DELETE on `app_user`, which no code used.
- **One row is a unique index on `(true)`**, as Armature holds its
  installation: an upsert names it with `ON CONFLICT ((true))`. A group role
  needs the provider and an Armature token the connection, and removing
  either takes them, by trigger, as their keys did.
- **Files keep `org/<installation id>/`.** An attachment's key was a column
  generated from `org_id`; a trigger gives it the same key now, so every
  stored file still resolves, and a tombstone may name only the
  installation's files.
- **The way back loses little.** Down gives every role its membership row,
  dated when the role was given, the installation its slug `exciter`, and
  every table its `org_id`; when a membership last changed was not kept.

## 2026-10-14: The code stops carrying the organization (#107)

With the schema no longer asking (#106), the organization in the code was
only weight: `internal/tenant`, the `app.org_id` setting, `events.Event.OrgID`
and an `org_id` in every insert and filter. They went, as Armature's #218
removes them.

- **A transaction says only who acts.** `inTx` sets the acting person, and a
  token's projects, whenever the context has somebody; there is no
  organization to refuse a transaction without. What that refusal guarded is
  the permission policies' to guard, and a transaction acting for nobody
  reaches nothing walled, which a test shows through the service and one
  straight through SQL.
- **What was sealed with the organization's id still opens.** The provider's
  client secret, Armature tokens and the webhook secret are sealed with the
  installation's id, the same bytes as before, read with the connection or
  through `installation.ID`. Theme files keep their prefix for the same
  reason. Armature's cache keys lost the organization, since a token id is
  already unique; entries under the old keys age out.
- **The principal is the person and their role.** It carries the
  installation's name for the shell and the role, empty for somebody who no
  longer belongs, which the middleware refuses as switched off. Admitting a
  join request answers `{userId, role}`.
- **`current_org_id()` answers the installation** (00414) rather than a
  setting nobody sets any more, so what still calls it outside the code keeps
  working until #108 drops it.
- **Audit retention runs once,** not once per organization.
- **A unit test holds the line:** no product Go file names `org_id` or
  `current_org_id`. The tests' own SQL still names the column, which #108
  drops with the tables.

## 2026-10-14: Row security keeps the walls, not the organization (#106)

The tenant policies kept one organization apart from none, so they went: each
table's became one permissive policy for every role, `USING (true)`, and the
admin role's bypass went with it as redundant. Armature dropped row security
altogether at the same step; Exciter keeps it, because its walls are its 109
restrictive permission policies, all for `exciter_app`, and FORCE keeps them
on the table owner too.

- **The admin role passes through the open policy**, as every role does; the
  permission policies never name it. `BYPASSRLS` would need a superuser to
  grant at every install and would exempt the role from what a later wall
  might want of it. Where the admin role alone reads, the app role holds no
  privilege (`oidc_login`, sessions, identities) or a policy that is not open
  (`app_user`, members only).
- **What the tenant policies hid by accident is now said on purpose.** A
  connection acting for nobody reads the installation and its people, which
  are everyone's, and nothing walled. The app role may no longer make or
  remove an organization, nor read a sign-in in flight, which it never needed.
- **Keys are the installation's.** Every key, foreign key and index was made
  again without `org_id`, under its name or its name without the word org,
  from the catalog, with a check first that names rows that would share a new
  key; up then down gives back the same catalog.
- **Functions keep their rules.** 58 functions lost the organization from
  their filters, joins, locks and inserts; three that took it as an argument
  were made again without it. Rows they write take `org_id` from a default,
  `installation_org_id()`, moved here from #107 so no function has to name
  the column.
- **What still names it** until #108: `current_org_id()`, the organization's
  own seed, the session's column, the provider's and the Armature
  connection's one-row keys, the foreign keys to the organization, and an
  attachment's object key, which #108 settles. A test lists them and fails
  when one stops naming it.

## 2026-10-14: Integration tests keep apart in one installation (#90)

The integration suite gave every test an organization of its own, which the
schema cannot offer once `org_id` goes (#91). Now every test works in the
stack's installation and keeps apart by what it makes, as Armature's #217
decided.

- **One at a time, as before.** The suite never called `t.Parallel`, so a
  test meets another's leftovers rather than its changes. Keys carry a mark
  of the test's, people unique addresses, and the end of a test removes its
  projects, newest first since a moved requirement names the project it left,
  and then its people. A cleanup that fails now fails its test instead of
  leaving rows for the next one.
- **Shared settings are put back.** Tests that change the link types for all
  projects, the default theme, the Armature connection, the identity provider
  or the installation's name snapshot it and restore it; a list names each
  with its reason and a test holds the list to the code. A run leaves the
  installation as it found it.
- **What only two organizations proved went.** `TestTenantIsolation`, the
  cross-organization raw-SQL cases and the API's "another organization" cases
  were deleted. Where they also proved a wall inside an installation, the
  stranger became a guest no grant names, which the database refuses the same
  way. The cases that still hold while tables carry `org_id`, nothing visible
  without the installation and every way around the policies refused, stay in
  `tenant_context_test.go`.
- **Databases of their own** for what one installation cannot show: the
  upgrade guard, a first start, and the last active administrator, which the
  shared installation always has more than one of.
- **The test endpoints went.** `POST` and `DELETE /test/orgs`,
  `internal/testorg` and `EXCITER_TEST_ENDPOINTS` served the browser suite
  until #83 and nothing since; they went, and with them the sign-in seam
  `Server.SignInOrg` of #85.

**Reconsider if** the suite grows slow enough to want `t.Parallel`: the
list of installation-wide tests is then the list of those that run alone.

## 2026-10-14: One installation is one team's requirements (#89)

Exciter is self-hosted, and an installation serves one team, as Armature and
Stator decided for theirs. Organizations were a layer such an installation
never used: naming one at sign-in, switching between them, a provider and a
slug each, an owner rule per organization, and one copy per organization of
settings there is really one of. They are gone from everything people and the
API see (#82): the first start makes the installation, a sign-in lands in it,
letting somebody go switches them off, the Armature connection is an address,
and the product says "everyone", "all projects" or "Exciter". People who must
not see each other's work run an installation each.

Several organizations in one database are refused at the upgrade rather than
merged (#84). Their project keys, statuses, workflows, link types, roles and
people would collide, every rule for which one wins would be wrong for
somebody, and a migration would be choosing for them with nobody watching.
Refusing leaves the choice with the operator, and the refusal names the
statement that deletes the others.

Inside, the product went first and the schema follows. The one organization
row still holds the installation's name and default theme, the tables still
carry `org_id`, and forced row level security on `current_org_id()` still
scopes every query, now to the one organization there is. That kept every
step shippable with all four test layers green. The integration suite keeps
its tests apart by organization until it isolates them another way (#90), and
the columns and tenant policies go with the schema ticket (#91); the
restrictive permission policies, which are Exciter's guards, stay.

This supersedes what the older entries say about organizations as part of
the product. An entry that calls labels, statuses, workflows, link types or
themes "the organization's", or takes a lock per organization, was written
while that was the installation's name, and its argument stands.

**Reconsider if** one installation routinely needs a group inside it that the
others must not see at all: that is a wall around people and settings, the
way a project is a wall around work, not organizations coming back.

## 2026-10-14: The screen says everyone, all projects or Exciter (#88)

An installation is one team's, so a setting that reaches everybody is "for
everyone", a link type that every project may use is "for all projects", and
a refusal reads "Only an administrator ...". Where the subject is the
installation itself, the copy names Exciter, as Armature's #215 names
Armature.

- **The theme source is `everyone`.** `GET /themes/active` answered
  `organization` for the default; nothing stores it, so it changed without a
  migration.
- **Checked, not remembered.** A vitest check walks both catalogues, calling
  every function with sample arguments, and refuses "organization" and
  "Organisation"; a Go unit test does the same for the API document's
  summaries, descriptions, tags and enums and the assistant's tool help, after
  Armature's.
- **What still says it.** Keys such as `ofOrg` and `data-*` hooks keep their
  names, as do the test-only endpoints, which #90 removes, and the code's own
  names, which #91 removes with the tenant layer.

## 2026-10-14: The Armature connection is an address (#87)

Armature decided, as Exciter did, that an installation is one team's tracker,
so the slug of an Armature organization no longer says anything the address
does not. The connection is its base URL alone: `armature_connection.org_slug`
went in 00403, and with it the form's field, the input and the slug in
`armature_endpoint()`. A new address is what forgets every member's token.

- **The learned organization stays the check.** Armature at 7920a98 still
  answers `principal.org` on `/auth/me`, so the first token that checks out
  teaches its id and every later token must match it, which keeps a token of
  some other Armature behind the same address from being stored.
- **A token that names no organization is let through.** Armature's #218
  drops the organization from its schema; once `/auth/me` stops reporting one,
  the address is all there is to go by, and refusing every token then would
  break the connection on Armature's upgrade.
- **The webhook is `POST /armature/webhook`.** Still pending until #29; the
  secret and the replay keys stay with the one installation row until #91.
- **The stub keeps its tenants.** They are a test device, picked by the token,
  so parallel tests stay apart; nothing in the product names one.
- **The M2 ranges moved** to 00420, 00430 and 00440, after the epic's 00400 to
  00419.

## 2026-10-13: Letting somebody go switches them off, and an active administrator stays (#86)

People belong to the installation, so there is no membership to take away
that leaves an account standing elsewhere. `DELETE /users/{userID}` keeps its
address and switches the account off, as Armature's #211 does: sessions end
and tokens are deleted at once, the account, its role, its groups and its work
stay, and `PATCH /users/{userID}` with `active: true` brings the person back.
A switched-off sign-in hears "Your account has been switched off. Ask an
administrator.", the same as somebody with no membership; the separate
`account_inactive` answer went.

- **The last owner rule became the last active administrator rule.** An
  installation is entered again through any administrator, owner or admin,
  so that is what the database keeps, on a role change, a membership going
  and an account switched off. The check takes an advisory lock, so two
  administrators switching each other off at once take turns and the second
  is refused. The service refuses the same first, with `last_administrator`.
- **Owners stay owners.** The bootstrap makes them through the admin role,
  and the app role neither makes nor unmakes one (a trigger, since a policy
  that hid owner rows would also hide them from `SELECT ... FOR UPDATE`).
  Only an owner switches another owner off, a rule of the service, as in
  Armature.
- **Nobody leaves through the app role.** It may no longer delete a
  membership; only the admin role, for tidying up, does.
- **Tokens are the person's.** Administrators see and revoke everybody's at
  `/tokens/everyone`, on the page `/settings/all-tokens`, which replace
  `/org/tokens` and `/settings/organization-tokens` without a redirect, as
  nothing shipped with them.

## 2026-10-13: A sign-in lands in the installation, and keeps where it opened (#85)

Nothing switches a session any more: the sign-in page asks for no
organization, single sign-on starts at `/auth/oidc/start` with the
installation's provider, and `POST /auth/switch-org`, the switchers and the
organizations on `/auth/me` went. With them went what decided where a session
might go: `session_reaches`, its widening to providers of the same issuer and
to organizations the person owns, `session_stays_home` and the organization a
proof was made for.

- **The session keeps the organization it opened in.** Armature resolves a
  session to the person's first membership. Exciter's integration suite lets
  the same Keycloak people into organizations of its own beside the seeded
  installation, so the oldest membership would land a test's sign-in in the
  wrong place. `current_org_id` is written once when the session opens and a
  trigger, `session_keeps_its_opening`, refuses moving it; #91 drops it.
- **A seam for the suite, until #90.** `Server.SignInOrg` names whose provider
  single sign-on starts with; nil, as everywhere but the integration suite,
  is the installation's, the oldest organization, as the seed finds it.
- **No redirect.** Armature redirects its old address for a release; no
  release of Exciter shipped with one, so it is simply gone.

## 2026-10-13: One installation holds one organization, and a database with several stops (#84)

Exciter is self-hosted, and an installation serves one team, as Armature and
Stator have decided (#82); more than one team takes more than one
installation. The organization row stays inside for now, so `org_id` and row
security keep working, and `internal/installation` is the one way to its name
and default theme; moving them off `org` changes that package and nothing
that calls it. The seed makes the one organization on a first start, called
`EXCITER_NAME`, under the fixed address `installation.Slug`, and finds it on
every later start whatever it was renamed to.

- **Several are refused, not merged.** Their project keys, statuses,
  workflows and people would collide, and no rule for which one wins would be
  right for everybody. Migration `00400` counts the organizations and, with
  more than one, stops, naming the first ten. No release of Exciter could
  delete an organization, so the refusal names the statement that does. It
  lifts forced row security on `org` to count, so an owner that is not a
  superuser sees the rows too.
- **Everybody signed in belongs.** Somebody signed in without an
  organization saw an empty shell, and every route had to ask whether there
  was a tenant at all. A password sign-in of somebody with no membership is
  refused with "Your account has been switched off. Ask an administrator.",
  and an open session whose membership went ends rather than being refused,
  so the browser goes back to sign in and hears it there. `requireOrg` and
  the `no_organization` error went with the case they covered.

**Reconsider if** somebody with several organizations asks for a way across
better than deleting: an export of one organization's projects into another
installation is the shape that would take.

## 2026-10-13: Browser specs keep to people and keys of their own (#83)

An installation serves one team (#82), so the browser suite stops making an
organization per spec file, as Armature's did (Cloudster1/Armature#209).
Supersedes "Each browser spec file gets a throwaway organization (#9)".

- **People of their own.** Each spec file gets, per worker, an alice who
  administers and a bob who is a member, made in `demo` by the bootstrap owner
  through `POST /users`, signed in with their password and let go when the
  worker stops (`e2e/fixtures/people.ts`). What they choose, the tokens they
  make and what they are granted is nobody else's.
- **Keys and words of their own.** A spec's projects carry keys no other worker
  or run draws (`uniqueKey`), and what every project shares, a label or a link
  type, gets a word of the worker's own. Search is narrowed to the spec's
  project, and the audit log to the target the spec planted.
- **The built-in look, chosen.** The default theme is everybody's. Each spec's
  people choose the built-in theme when they are made, so a default the
  themes spec sets meanwhile never reaches another spec's contrast check.
- **A lane of one worker for what there is one of.** The provider, the default
  theme and the Armature connection are the installation's, so the sso,
  themes, armature and armature-links specs run in a Playwright project of one
  worker, beside the rest rather than after them: Playwright runs the
  projects another depends on whole in every shard, which would have run the
  whole suite twice in CI. Demo's own alice is only the sso spec's now, which
  changes her role and points demo's provider at a dead issuer, and restores
  both.

## 2026-10-13: One remote link per requirement and issue, at its permalink (#25)

Stator puts one remote link per page and issue at the page's address. A
requirement may be linked to the same issue with two relations, say implements
and verifies, and Armature keys remote links by url, so a second link at the
same url would retitle the first. One remote link stands for the pair, titled
with the first relation, and its url is the requirement's permalink
`/r/{id}`, which survives a move to another project and a new project key
where `/p/{key}/r/{key}` would not. Until Armature names a remote link's kind,
requirements appear in its Pages panel told apart by their source alone; the
contract lists that as something Exciter would like, not something it needs.
**Reconsider if** Armature gives remote links a relation of their own.

## 2026-10-13: Project templates live in code (#24)

The issue named a `project_template` table, but the three templates are part
of the product, not of an organization, and a table would only hold a copy of
the code that writes them. They are in `internal/project/templates.go`, and a
project keeps only the key it began from. A template is applied once, in the
transaction that makes the project, so changing one never changes a project
made earlier. The requirement package makes the sections through a seeder the
project service is given, since project cannot import it.
**Reconsider if** organizations want templates of their own, which would
belong in a table.

## 2026-10-13: A key of another project names a row of the file (#23)

The contract said an import refuses a key of another project. But a
project's export carries its own keys, and importing it into an empty
project, which the integration suite proves, needs those keys to say which
row is a parent of which and where each link goes. So a key of another
project names its row within the file and the row makes a new requirement,
while a key of the project itself still changes that requirement, and one of
the project that is not there is still refused. The status of a new
requirement is the one other thing an import may set that no save does; a
database function allows it only for a requirement made in the same
transaction, by somebody who administers its project.
**Reconsider if** people mean to update another project's requirements by
importing into this one.

## 2026-10-13: The database holds a token to its projects (#22)

A token limited to projects could be held there by the service alone, as
Armature does, but every query that forgot to ask would then reach further.
As Stator does for spaces, the transaction carries the token's projects in
`app.token_projects`, and `perm_project_holds`, which every project policy
calls, refuses any other project; `current_actor_administers` refuses the
organization as a whole. The limit is the caller's alone, so a rule asked
about somebody else still answers for them. The API refuses the operations
marked `orgWide` first, with a sentence that says why, by matching the
request against its own router rather than listing paths twice.
**Reconsider if** a project policy stops going through `perm_project_holds`.

## 2026-10-13: A file is kept by its own requirement's revisions (#21)

A revision is never changed, so a file it shows has to stay for as long as the
revision can be read, which is as long as its requirement exists. Deleting
such a file takes it off the list and keeps the row and the bytes; a trigger
refuses deleting the row while a revision of the requirement names it, and a
purge takes the requirement, its revisions and its files together. Only the
file's own requirement's revisions count: a copy points its revision 1 at
files of its own, and a move keeps the requirement and with it the object
key. A save does not check that a picture belongs to the requirement, as
Stator's does not, so a picture pasted from another requirement shows while
that file lasts but keeps nothing.
**Reconsider if** people paste pictures between requirements and lose them.

## 2026-10-13: The database walks every link for a circle (#18)

A link shows only to whoever views both its ends, so a circle can run through
a requirement the writer cannot see. A check that walks only what the writer
sees would let two people, each blind to the other's project, close a circle
of a type that allows none. The trigger on `trace_link` therefore runs as the
owner and walks every live link of the type, under a lock per organization so
two links cannot close one circle together. The service walks first over what
the caller sees, to name the way round; when only the trigger finds the
circle, the refusal names no requirement on it. Whether a project uses a type
and which pairs it allows stay in the service: they are a project's
preference, not something history or coverage relies on.
**Reconsider if** link writes queue behind the organization's lock.

## 2026-10-13: Search judges rights a project at a time (#20)

Row level security runs a restrictive policy on every row a query reads before
a non-leakproof operator such as `@@` may filter, so a search through the
policies alone would check the reader's rights on every requirement of the
organization, however few matched. Stator met the same cost and answered it
with `page_search_rows`; Exciter does the same with `requirement_search_rows`,
a `SECURITY DEFINER` function that finds the words through the index and then
asks `perm_project_holds` once per project the hits lie in. It applies what
the tenant and viewer policies apply, out of the trash, and the integration
suite proves through raw SQL that it returns nothing more.
**Reconsider if** a requirement gains rights of its own beyond its project's.

## 2026-10-12: The database takes a transition whole (#17)

A reviewer approves without holding edit, so the app role may neither write
the requirement's change log nor its owner for them. Rather than loosen those
policies for approvers, `requirement_transition` writes the status, the log
entry and the owner its post-functions set, reading the post-functions from
the transition's own rules. The service still runs the conditions and
validators first, for the sentences a refusal needs, but what a transition
does to the row is decided where nobody can ask for something else.
**Reconsider if** a post-function needs more than the owner, such as a value
an approver may not write.

## 2026-10-12: A project key can change, and the old one keeps resolving (#10)

Neither Armature nor Stator lets a project or space key change. Exciter does,
because a specification often outlives the name it began with, and every
requirement key carries the project key. A changed key stays behind in
`project_key_alias`, so `CHK-R5` still finds `SYS-R5` in links pasted into
mail, Armature issues and Stator pages. A key is taken while any project has
it now or had it before, so an old key never points at two projects. A move to
another project keeps each requirement's old key in `requirement_old_key`, as
Armature's `issue_old_key` does.
**Reconsider if** aliases pile up enough that freeing old keys matters.

## 2026-10-12: Statuses and workflows belong to a project (#10)

Armature's statuses and workflows are the organization's, assigned through
schemes. Exciter keeps them per project, with no schemes: the project's
administrators own the whole of their process, and templates can set it up
without touching anybody else's. Questions across projects key on the five
status categories, which are fixed, never on status names.

## 2026-10-12: Project roles add up, and reviewers approve without editing (#10)

A grant gives one role per subject in a project, and a person holds the union
of every grant that reaches them. Reviewer and editor are separate roles
rather than steps on one ladder, so a reviewer can approve without being able
to change what they approve, and somebody who should do both holds both. A
reviewer takes only the transitions whose conditions name the reviewer role,
which is how the default workflow's Approve works.

## 2026-10-12: Sections are requirements of their own kind (#10)

A section is a row of `requirement` with `kind = 'section'`: a title, an
optional introduction, a key and revisions, and no type, status, attributes or
links. One table keeps one tree, one rank, one trash, one permalink and one
cycle guard. The price is that sections use up requirement numbers, which only
need to be unique, not dense in meaning.

## 2026-10-12: Retire rather than delete what history names (#10)

Types, attributes, options, statuses and link types that a requirement, a
revision or a link ever used are retired: they stay readable where they were
used and cannot be chosen again. Deleting is only for what nothing ever used.
A revision must read tomorrow as it read the day it was written, which a
deleted option or type would break.

## 2026-10-12: An import runs in the worker (#10)

Armature imports a CSV file within the request. Exciter's imports build a tree
and links in a second pass over files of up to 5000 rows, which can outlast a
request. So an upload only previews and checks. Starting hands the import to
the worker through the outbox, and the client polls its progress. Every
problem the file has is reported before anything is written, so a started
import refuses rows only when the project changed meanwhile.

## 2026-10-12: Each browser spec file gets a throwaway organization (#9)

Superseded by "Browser specs keep to people and keys of their own (#83)"; the
test endpoints went with #90.

As in Stator: specs that change what an organization shows, such as its
default theme or somebody's role, cannot share `demo` with specs that judge
the built-in look, and running them one after another only hides that. The
api has two test endpoints, `POST /api/v1/test/orgs` and
`DELETE /api/v1/test/orgs/{slug}`, and `e2e/fixtures/org.ts` makes one
organization per spec file and worker through them, signs alice and bob in to
it and deletes it when the worker stops.

- **Made like `demo`.** The organization gets the seed's people and provider,
  from the same `EXCITER_BOOTSTRAP_*` settings and `seed.Populate`, so it
  signs in exactly as `demo` does. Exciter lets only an administrator
  configure a provider, so the bootstrap administrator is made its owner
  first and the provider is configured in their name, as the seed does.
- **Compiled in, routed only when asked.** The image the suite runs is the
  image that ships, so the endpoints are in it, but routed only when
  `EXCITER_TEST_ENDPOINTS` is on; otherwise they answer 404. Every call
  carries `EXCITER_TEST_ENDPOINTS_TOKEN` in `X-Exciter-Test-Token`, so an
  endpoint switched on by accident is still closed, and the api refuses to
  start with them on in production. Deleting refuses any organization the
  endpoint did not make, which it marks in `org.settings`.
- **Not in the document.** They are left out of `api/openapi.json`: the
  document is what clients are generated from, and the web client would
  otherwise carry types for calls it must never make. A table of their own in
  `internal/httpapi/test_handlers.go` describes them, and a unit test holds
  the router to both tables and the document to the public one alone.

## 2026-10-12: The logo is a resized picture and the icons are a crop of it (#56)

Exciter's mark was a hand-drawn E on the accent. The picture now given is a
detailed illustration with small text, clear at a few hundred pixels and a
smudge at sixteen. It is handled as Stator handles its own:

- **Two kinds of image from one source.** The whole picture, resized to 128,
  256 and 512 pixels in WebP, is the logo, with `srcset` so a screen takes the
  one it needs. The favicon, touch icon, manifest icons and the sidebar's mark
  are a square cut of the gopher's face, which is what still reads small.
- **Made by a target, from a container.** `make branding` runs ImageMagick in
  an Alpine image and writes `web/public`, so nothing is installed on the host
  and `assets/branding/logo.jpg` stays the one thing to replace. The made
  files are committed, so a build needs no ImageMagick.
- **Served from the site's own origin.** `img-src 'self'` already lets them
  load, and nginx already names the manifest's type.

## 2026-10-12: Requirement keys are `CHK-R42` (#1)

A requirement's key is its project key, `-R` and a number per project:
`^([A-Z][A-Z0-9]{1,9})-R([1-9][0-9]*)$`. Armature's commit and mail scanners,
its query language and Stator's issue chips all look for a project key, a
hyphen and a digit, so none of them mistakes a requirement for an issue, and
the same project key can be used in Armature and Exciter without ambiguity.
Keys such as `SYS-12`, a type prefix and a number, are therefore not allowed.
A unit test feeds Exciter keys through copies of their patterns (#13).

## 2026-10-12: A revision is a normative save (#1)

A save writes an immutable revision when title, statement, rationale, type or
attributes change. Status, owner, labels and the place in the tree are logged
but never revisioned, so changing them neither voids an approval nor makes a
link suspect. Links are not part of a revision either: a revision of B for
every link to B would make everything below B suspect. Baselines capture the
link set instead. Editing an approved requirement is allowed; the new revision
supersedes the approval and the workflow takes the status back.

## 2026-10-12: The status category of a linked issue is stored for everyone (#1)

Stator shows Armature data only as the viewing person sees it. Coverage needs
to know whether an implementing issue is done for every reader, so Exciter
stores the status category of each linked issue, updated by Armature's
webhooks, and fetches summary and assignee live as the viewer.
**Reconsider if** Armature adds a way to answer status for an organization
without a person's token.

## 2026-10-12: Stack ports from 10000, and Armature removal wins (#1)

Each checkout's stack takes a slot of 20 ports from 10000 to 19999, derived
from its path. Stator uses 20000 to 29999 and the kernel hands out ephemeral
ports from 32768, so neither collides; slots holding 16686 (Armature's
Jaeger), 11211, 15672 or 10250 are skipped.

When someone removes Exciter's link from an issue in Armature, the trace link
is marked detached and not pushed again: the person's decision in Armature
wins over the sync.

## 2026-10-12: The tooling is Stator's (#1)

Exciter ports Stator's gate, hooks, release script and CI rather than
Armature's, because Stator's are the newer of the two and already split the
gate between a quick local run and CI. Stator is under Apache-2.0 since
`a5661a6`; `NOTICE` names the commit each ported file came from. A layer
joins the gate in the change that introduces it, so the gate is green from
the first commit.
