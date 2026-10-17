# API contract for M6: Document view

M6 makes the specification page of #15 the place a specification is written,
moved through its workflow and reviewed: requirements edited where they are
read, their states changed one at a time or many at once, what changed since
their previous revision, their last approval or a baseline marked in place,
and reviews decided entry by entry. The epic is #166; this file is #167. The
route table in `backend/internal/httpapi/operations.go` is the source of
truth for paths, fields and types, and this file says what they mean.
Operations it adds carry `pending: true` and answer 501 until their issue is
built; parameters and fields it adds to operations that exist arrive with
their issue.

All paths are under `/api/v1`. Every operation needs somebody signed in.
"view", "edit" and "approve" are the project rights of #12; a project or
requirement the caller may not view is 404, never 403, and a change in an
archived project is 409 `project_archived`. A refusal is a sentence that says
what to do, as in M1. A review stays one requirement at one revision, as #35
left it.

## Migrations, packages and settings

None. Everything M6 reads is stored already: revisions (#16), statuses (#17),
reviews (#35) and baselines (#37, #153). The bulk operations take each
requirement through the single operation's service and SQL function, so the
guards of `requirement_transition` and `requirement_review` hold for them
unchanged. No new settings, events, audit actions or worker topics: a bulk
transition or request emits, for each requirement, what the single one does.

Limits, in `internal/requirement`:

- `MaxSpecChangesLimit` = 50: one window of the specification with changes,
  each entry carrying its comparison.
- `MaxBulk` = 100: requirements named in one bulk read, transition or review
  request.

## The specification as #15 left it, and what changes

`GET /projects/{projectKey}/spec` keeps `limit` and `offset` and every field
of its entries. Each issue below adds to it; a client that sends none of the
new parameters gets #15's specification, with more fields in it.

- **Each entry gains**, from #168:
  - `type`: the requirement's type as a list shows it (`TypeRef`), null for a
    section or a requirement without one.
  - `owner` (`PersonRef`) and `priority`, null when unset.
  - `approval`: its review now open and the latest revision approved, as
    `GET /requirements/{reqKey}/approval` answers it; for a section, no review
    and nothing approved.

## #168 Editing in place

No new operation. An entry is saved with `PATCH /requirements/{reqKey}`,
sending the entry's `node.revision` as `baseRevision`; a newer revision is
409 `revision_conflict`, and the client reads `GET /requirements/{reqKey}/diff`
for the comparison it offers to save over. A requirement or section is added
with `POST /projects/{projectKey}/requirements` and its `place`: after an
entry is `{parentId: entry.node.parentId, afterId: entry.node.id}`, as its
first child `{parentId: entry.node.id, afterId: null}`.

- **UI.** Edit turns the entry into its editor in place; one entry at a time;
  leaving with unsaved changes asks first; the keyboard moves between entries
  and starts, saves and leaves an edit.
- **As built** (#168):
  - `RequirementEdit` moved to `web/src/features/requirements/RequirementEdit.tsx`
    and edits an `Editable`, which a requirement and a specification entry
    both give; the new requirement form's fields are `RequirementCreate`,
    which takes a place. On a conflict both pages show what the newer
    revision changed since the draft was opened.
  - A requirement added after an entry is written below the entry's last
    descendant, where the server places it. Leaving the page with a changed
    draft is held by the router's blocker, which also covers closing the tab.
  - Keys: the arrow keys, Home and End move between entries, Enter or E
    edits, A adds after; in a form Ctrl or Cmd S saves and Escape leaves,
    unless the editor's own popup took the key.

## #169 States from the document

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/transitions` | view | `{requirements: [{key, transitions}]}`; 422 |
| `POST /projects/{projectKey}/transitions` | view | `{results}`; 409 `project_archived`; 422 |

- **The read** takes `key`, repeated, at most `MaxBulk`, and answers for each
  requirement the transitions the caller may take now, as
  `GET /requirements/{reqKey}/transitions` would. A key the caller may not
  view, of another project, of a section or in the trash is left out. No key,
  or more than `MaxBulk`, is 422 on `key`.
- **The transition** takes `{transitionId, comment, keys}` and takes that
  transition on each requirement in its own transaction, in the order given,
  through `requirement_transition`, as
  `POST /requirements/{reqKey}/transitions` would. One refused undoes nothing
  else. The answer is 200 with a result per key, in the order given:
  - `{key, outcome: "taken", requirement}`, the requirement as it is now;
  - `{key, outcome: "refused", error: {code, message}}`, with the single
    operation's code: `not_found`, `forbidden` (the transition is not the
    caller's to take), `transition_unavailable` (it does not leave the
    requirement's status now, which covers one that moved on in between), or
    `validation_failed` with the validator's sentence (a comment or an owner
    required, required attributes missing).
  An empty `keys`, more than `MaxBulk` or a key named twice is 422 on `keys`.
- **The specification gains** `status` and `category`, each repeated: keep
  the requirements in one of those statuses or status categories, and the
  sections above them so the outline reads; every entry when both are absent.
  `total` counts what is kept. An unknown status or category is 422.
- **UI.** A status badge opens the requirement's transitions; selected
  entries offer the transitions every one of them has, by id, and report each
  requirement's outcome; the status filter is part of the page's address.
- **As built** (#169):
  - `requirement.Service.TransitionsOf` reads every key in one transaction
    with the single read's `offer`; `BulkTransition` checks the keys, the
    transition and the comment once, refuses an archived project whole, and
    takes each key through `transition`, which holds it to the project, so a
    key of another project is `not_found`. The handler answers each refusal
    with `refusalOf`, the single operation's code and sentence, a refused
    field's sentence in place of "Some fields need attention".
  - The spec's filter is a recursive `above` over `parent_id` from the kept
    requirements, of which only sections stay, so a requirement outside the
    filter never shows for its child's sake.
  - The page keeps one selection: entries are picked for a transition, and
    for Armature issues where Armature is connected, and the bar offers both.
    A section's checkbox picks what is loaded below it.

## #170 Changes in the document

- **The specification gains** `changes`: `previous`, `approved` or
  `baseline`, and with `baseline` the parameter `baseline`, a baseline of the
  project by id (422 when it is missing, of another project or not viewable).
  With `changes`, `limit` is at most `MaxSpecChangesLimit`, and:
  - **Each requirement entry gains** `change`, null for a section:
    - `kind`: `changed`, `unchanged`, `added` (no earlier side: revision 1
      against `previous`, never approved against `approved`, not in the
      baseline) or `moved` (against a baseline: the same revision elsewhere in
      the outline; a revised and moved one is `changed` with `movedFrom`).
    - `from` and `to`: the revisions compared, `from` 0 for `added`.
    - `movedFrom`: its outline in the baseline when it stood elsewhere there,
      otherwise null.
    - `comparison`: what `GET /requirements/{reqKey}/diff?from=&to=` answers,
      null when `unchanged` or `moved`.
  - `onlyChanged=true` keeps the requirements whose `kind` is not
    `unchanged`, and the sections above them; `total` counts what is kept.
  - **The answer gains** `changes: {base, baselineId, changed, removed}`:
    `changed` counts the changed, added and moved requirements of the whole
    document, and `removed`, with `baseline` only and on the window at offset
    0, lists the baseline's requirements no longer in the document (moved
    away, in the trash, or deleted), each `{key, title, outline, revision}`
    as the baseline held it.
- **`Comparison` gains** `title`: the title as one paragraph block, with
  inline `diffInsert` and `diffDelete` marks when it changed, so every
  comparison, here and in `GET /requirements/{reqKey}/diff`, marks the title's
  words as it marks the statement's.
- **UI.** A Changes switch with a base picker; changed entries show inline
  diffs of title, statement, rationale and attributes; only changed collapses
  the rest, with next and previous change; all of it in the page's address;
  each changed entry links to its full comparison.
- **As built** (#170):
  - `requirement/changes.go` classifies every requirement of the project in
    one CTE, `chg`, per base, which the window, `onlyChanged` and the count
    of `changed` share; a query's parameters are numbered as its text asks
    for them, so a base that needs no baseline sends none.
  - Moved against a baseline is another parent or rank, as
    `baseline.Classify` decides it, so a requirement renumbered by one added
    before it is not moved. A requirement outside a section baseline's
    section is added.
  - `comparison` loads the project's attribute definitions once for the
    window; `Compare` shares it. The title is diffed as a one-paragraph
    document.
  - The page's Only changed collapses unchanged entries to their headings
    in place, so the outline still reads, rather than sending `onlyChanged`;
    the parameter stays for other clients.

## #171 Reviews in the document

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/reviews` | view | `{results}`; 409 `project_archived`; 422 |

- **The request** takes `{items: [{key, revision}], comment}` and asks for a
  review of each requirement at the revision the caller saw, in its own
  transaction, as `POST /requirements/{reqKey}/reviews` would; one refused
  undoes nothing else. The answer is 200 with a result per item, in the order
  given:
  - `{key, outcome: "requested", review}`;
  - `{key, outcome: "refused", error: {code, message}}`, with the single
    operation's code: `not_found`, `forbidden` (the caller may not ask for it),
    `section_not_reviewed`, `no_approval_rule` (nobody approves requirements
    of its type), `review_pending` (one is open already) or
    `revision_moved_on` (a newer revision than the one named).
  Empty `items`, more than `MaxBulk` or a key named twice is 422 on `items`.
- **Each requirement entry of the specification gains** `review`, from #171:
  null without a pending review, otherwise `{id, revision, required,
  approvals, rejections, approver, decision}`, where `approver` says the
  caller is one of its frozen approvers and `decision` is the caller's
  standing decision, `approve`, `reject` or null.
- **The specification gains** `waiting=true`: keep the requirements whose
  pending review waits for the caller's decision, and the sections above
  them, as `GET /reviews/waiting` would count them. Combined with
  `changes=approved` it is review mode.
- **Deciding** is `POST /reviews/{reviewID}/decisions` as it is, naming the
  entry's `review.revision`; a superseded review answers 409
  `revision_moved_on`, which the entry says. Comment threads are
  `GET` and `POST /requirements/{reqKey}/comments` as they are; a new thread is
  written on the requirement's current revision, which is the one under review
  while the review is pending.
- **UI.** Review mode shows the entries waiting for the caller with their
  changes since their last approval; approve and reject sit on each entry,
  with the signature where approvals are signed; a decided entry stays in
  view, marked, until review mode ends; a count says how many are decided out
  of how many; an author asks for review of the changed requirements of a
  section, or of the selection, in one step. `/reviews` opens each project's
  document in review mode.

## What is left for later

- A review of a document or a section as a whole, decided once for every
  requirement in it: M6 decides many reviews in one pass instead.
- Inline comments anchored to words of a statement, still deferred as in M3.
- A Word or PDF file of the document with its changes marked.
