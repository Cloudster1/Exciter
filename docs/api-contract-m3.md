# API contract for M3: review and control

The agreement between whoever builds the backend and whoever builds the web
client of #33, #34, #35, #36, #37 and #38. As in `docs/api-contract-m1.md`, the
route table in `backend/internal/httpapi/operations.go` is the source of truth
for paths, fields and types, and this file says what they mean. Every
operation below is in the table with `pending: true` and answers 501
`not_implemented` until it is built; whoever builds one removes the mark,
routes its handler and keeps this file true. The types live in
`internal/comment`, `internal/watch`, `internal/notify`, `internal/review`,
`internal/baseline`, and `internal/trace` (`suspect.go`, `coverage.go`).

All paths are under `/api/v1`. Every operation needs somebody signed in.
"view", "comment", "edit", "approve" and "administer" are the project rights of
#12, which add up across grants; a requirement or project the caller may not
view is 404, never 403, and one of an archived project refuses every change
with 409 `project_archived`. A refusal is a sentence that says what to do, as
in M1.

Comments, watching and notifications follow Stator's (`docs/api-contract-m2.md`
at cf0f6f7, its #22, #24, #25 and #26), over requirements rather than pages.
Reviews take the shape of Stator's page reviews (#313) for a different act: a
Stator review approves a draft before it is published, an Exciter review signs
off a revision that already is.

## Migrations, packages and settings

| Range | Owner | Holds |
|---|---|---|
| 00500 to 00509 | comments (#33) | `comment` (one row per comment: thread, requirement, revision, body, edits and deletion), its policies, write guard and events |
| 00510 to 00519 | watching and notifications (#34) | `watch`, `watch_optout`, `notification`, `notification_preference`, `notification_digest` |
| 00520 to 00539 | review (#35) | `approval_rule`, `approval_rule_subject`, `requirement_review`, `review_subject`, `review_decision`, the workflow condition's SQL |
| 00540 to 00549 | suspect links (#36) | the view `trace_link_state`, `trace_link_clearance`, `armature_issue_link.reviewed_rev` |
| 00550 to 00569 | baselines (#37) | `baseline`, `baseline_item`, `baseline_link`, `baseline_external_link` |
| 00570 to 00579 | coverage (#38) | `coverage_rule` |

The issues name 00400 to 00470; those numbers went to the one-installation work
(#82) and M2, so each range moved up by a hundred, and goose never meets a
lower number after a higher one. No new table carries `org_id`.

New packages: `internal/comment`, `internal/watch`, `internal/notify`,
`internal/mail` (Stator's, plain SMTP), `internal/review`, `internal/baseline`;
suspect links and coverage go into `internal/trace`.

New settings:

| Setting | Meaning |
|---|---|
| `EXCITER_SMTP_ADDR` | Where the worker sends mail, `host:port`, without authentication, as Stator's. Empty sends none: notifications are still written, and the worker says once that mail is off. The stack points it at Mailpit. |
| `EXCITER_MAIL_FROM` | The sender of every mail. Required with an SMTP address. |

### Events

Every payload carries `actorId`, as the outbox's policy asks. Topics are added
to `events.Topics`.

| Topic | When | Payload |
|---|---|---|
| `comment.created` | a thread started or a reply | `commentId`, `threadId`, `requirementId`, `reply`, `mentioned` |
| `comment.edited` | a comment rewritten | `commentId`, `threadId`, `requirementId`, `mentioned` (only those added) |
| `thread.resolved`, `thread.reopened` | a thread resolved or reopened | `threadId`, `requirementId` |
| `review.requested` | a review asked for | `reviewId`, `requirementId`, `revision` |
| `review.decided` | a decision, and the state it left | `reviewId`, `requirementId`, `decision`, `state` |
| `review.closed` | a review withdrawn or superseded | `reviewId`, `requirementId`, `state` |
| `link.cleared` | a link or an Armature link confirmed | `linkId` or `armatureLinkId`, `requirementId` |
| `baseline.taken` | a baseline taken | `baselineId`, `projectId` |

`requirement.revised`, which exists, is what makes a link suspect: the
notifier finds the links it turned.

New audit actions: `comment.deleted` (another person's comment, by an
administrator), `approval_rule.saved`, `approval_rule.removed`,
`review.withdrawn` (by an administrator), `baseline.taken`, `baseline.locked`,
`baseline.deleted`, `coverage_rule.saved`, `coverage_rule.removed`.

## #33 Comments and mentions

| Operation | Needs | Answers |
|---|---|---|
| `GET /requirements/{reqKey}/comments` | view | `{threads}`, oldest first |
| `POST /requirements/{reqKey}/comments` | comment | 201 `{thread}`; 409 `requirement_trashed`, `project_archived`; 422 |
| `GET /requirements/{reqKey}/mentionable` | comment | `{people}`: `{id, name, email, canView}` |
| `GET /comments/{commentID}` | view | `{thread}` |
| `POST /comments/{commentID}/replies` | comment | 201 `{comment, thread}` |
| `PATCH /comments/{commentID}` | the author | `{comment}`; 403 |
| `DELETE /comments/{commentID}` | the author, or administer | 204; 403 |
| `PUT /comment-threads/{threadID}/resolution` | comment | `{thread}` |
| `DELETE /comment-threads/{threadID}/resolution` | comment | `{thread}` |

- **Threads, one level deep.** A thread is its first comment and the replies
  after it; its id is the first comment's. A reply to any comment of a thread
  goes to its end. A reply to a resolved thread reopens it, without an event.
  Sections take comments as requirements do.
- **Each comment records the revision** the requirement had when it was
  written, read under the requirement's lock, with a foreign key to
  `requirement_revision`. The page shows "on revision 3" when that is not the
  current one.
- **The body** is a document of the requirement editor's comment subset:
  paragraphs, lists, quotes, code, links, the four marks, and `mention`. At
  most `comment.MaxBodyBytes`, 64 KiB; a body with no text is 422 on `body`.
- **Mentions are read from the document**, as Stator's: a `mention` node
  names a person by id. Who may be told is a member who may view the
  requirement; the event carries them in `mentioned`, the database refuses an
  event naming anybody else, and an edit tells only those it adds.
- **Edit and delete.** Only the author rewrites a comment, while they may
  still comment; it gains `editedAt`. The author, or anybody who administers
  the project, deletes one: the body goes, the place and the author's name
  stay, and the notifications about it go with it. A thread whose every
  comment is deleted is not listed. An administrator deleting another
  person's comment is audited.
- **Resolving** is anybody's who may comment, and says who and when.
- **In the database**, as Stator's 00140 and 00155: the app role may insert a
  comment of its own on a requirement it may comment on, and change only the
  body, the edit and the deletion columns; a guard trigger refuses anything
  else, the thread and requirement never change, and a deleted comment is
  frozen. Each is proven through SQL as `exciter_app`.
- **Changed:** a requirement gains `comments: {threads, open}`, and the grid
  can show the open count.
- **Tools:** `list_comments` and `comment_on_requirement` once built.
- **UI:** a Discussion panel on the requirement page, the mention picker in
  the comment editor, and the open count in the specification view.

## #34 Watching and notifications

| Operation | Needs | Answers |
|---|---|---|
| `PUT /requirements/{reqKey}/watch` | view | `{watching}`: `{requirement, project}` |
| `DELETE /requirements/{reqKey}/watch` | view | 204 |
| `GET /requirements/{reqKey}/watchers` | view | `{watchers, total, limit, offset}` |
| `PUT /projects/{projectKey}/watch` | view | `{watch}` |
| `DELETE /projects/{projectKey}/watch` | view | 204 |
| `GET /watches` | anybody | `{watches, total, limit, offset}` |
| `GET /notifications?unread` | anybody | `{notifications, total, limit, offset}` |
| `GET /notifications/unread-count` | anybody | `{unread}` |
| `POST /notifications/read` | anybody | 204 |
| `GET /notification-preferences` | anybody | `{preferences}` |
| `PUT /notification-preferences` | anybody | `{preferences}`; 422 |

- **What is watched.** A requirement by itself, or a whole project. A
  watcher's own watches are theirs alone to read and change.
- **Watching on its own.** Writing a requirement, saving a revision of it
  and being made its owner make that person watch it, unless they unwatched it
  before or switched `autoWatch` off. Unwatching records the choice, so
  editing again does not undo it; watching again clears it. Commenting does
  not make a watcher: a commenter hears about their threads through
  `replied` and `resolved`.
- **Kinds**, and who is told; a person named by one event several ways gets
  one notification, of the first kind in this order:

  | Kind | Event | Who |
  |---|---|---|
  | `mentioned` | `comment.created`, `comment.edited` | those mentioned |
  | `review_requested` | `review.requested` | the frozen approvers |
  | `reviewed` | `review.decided` that finished a review | who asked for it, and the revision's author |
  | `suspect` | `requirement.revised` that turned links suspect | the owner and watchers of each link's other end |
  | `replied` | a reply | the authors of the thread |
  | `commented` | `comment.created` | the requirement's watchers |
  | `resolved` | `thread.resolved`, `thread.reopened` | the authors of the thread |
  | `revised` | `requirement.revised` | the requirement's watchers |
  | `transitioned` | `requirement.changed` of the status | the requirement's watchers |

  The actor is never told of what they did. Each notification is written
  acting as its recipient, so the policy refuses one about a requirement they
  may not view.
- **Reading.** Opening a notification marks it read, and so does
  `POST /notifications/read` with `ids` or `all`; nothing else does. The bell
  asks `unread-count` every half minute and on focus; there is no push.
- **Preferences.** `inApp` and `email` switch each kind, a kind left out
  being on; mail goes only for a kind that is on in the app as well. `digest`
  is `off` (a mail per notification, sent once the row is written),
  `hourly`, or `daily` at `notify.DailyDigestHour`, 08:00 UTC. A digest
  leaves out what was read meanwhile or may no longer be viewed, and nothing
  is sent when nothing is left. Mail is sent once and never retried, plain
  text, with a link to the requirement and to the preferences.
- **In the database**, as Stator's 00151 and 00152: a person reads and writes
  only their own watches, opt-outs, notifications and preferences; the app
  role may change only `read_at` of a notification, and one row per event and
  person makes a repeated delivery harmless.
- **Changed:** a requirement gains `watching: {requirement, project}`.
- **UI:** a Watch button on the requirement and the project, the bell in the
  top bar with the latest notifications, `/notifications`, and the
  preferences under Your account.

## #35 Review and approval of requirement revisions

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/approval-rules` | view | `{rules}` |
| `PUT /projects/{projectKey}/approval-rules` | administer | `{rule}`; 403, 422 |
| `DELETE /approval-rules/{ruleID}` | administer | 204; 403 |
| `GET /requirements/{reqKey}/approval` | view | `{approval}`: `{pendingReviewId, approvedRevision, current}` |
| `GET /requirements/{reqKey}/reviews` | view | `{reviews, total, limit, offset}` |
| `POST /requirements/{reqKey}/reviews` | edit | 201 `{review}`; 409 `no_approval_rule`, `review_pending`, `revision_moved_on`, `requirement_trashed`; 422 |
| `GET /reviews/waiting` | anybody | `{reviews, total, limit, offset}`, oldest first |
| `GET /reviews/{reviewID}` | view | `{review}` |
| `POST /reviews/{reviewID}/decisions` | a frozen approver | `{review}`; 403 `not_an_approver`, `own_revision`; 409 `review_closed`, `revision_moved_on` |
| `POST /reviews/{reviewID}/withdraw` | who asked, or administer | `{review}`; 403; 409 `review_closed` |

- **Rules.** A project has at most one rule per requirement type and one for
  every type without its own. A rule names its approvers, people or groups
  (never everyone), how many approvals it takes (`review.MinRequired` to
  `MaxRequired`, 1 to 10), and whether the author of the revision may approve
  it. A requirement whose type has no rule, and no project rule covers, is
  refused a review with 409 `no_approval_rule`.
- **Asking.** Whoever may edit a requirement asks for its current revision,
  naming the revision they saw: another one is 409 `revision_moved_on`. The
  review pins that revision and freezes the rule's approvers and count, so a
  later rule change leaves it alone. One review is pending per requirement at
  a time (409 `review_pending`); sections take none.
- **Deciding.** An approver frozen on the review, directly or through a group
  they belong to, who may view the requirement, approves or rejects, naming
  the revision they read: another one is 409 `revision_moved_on`. The author
  of the pinned revision is refused with 403 `own_revision` unless the rule
  allowed it. A later decision of the same approver replaces theirs. One
  rejection closes the review as `rejected`; the approval that reaches the
  count closes it as `approved`. A comment is optional, at most 500
  characters.
- **Superseding.** A new revision of the requirement makes its pending review
  `superseded`; an approved one stays approved, of the revision it named, and
  stops counting (`current: false`). The workflow takes the status back as
  its step's `on_revision_status_id` already says (#17).
  Withdrawing is for whoever asked, or the project's administrators.
- **The workflow condition** `condition.approved` lets a transition through
  only when the requirement has an approved review of its current revision.
  It joins the conditions of `GET /workflow-rule-types`; the default
  workflow's Approve transition does not use it, so a project turns it on.
- **The inbox** `/reviews` lists what waits for the caller's decision, across
  every project they may view, oldest first.
- **In the database:** a decision is written only by a frozen approver, not
  by the author unless allowed, and only while the review is pending and its
  revision is the requirement's; a review's state moves only forward; a
  revision's trigger supersedes. Each is proven through SQL as `exciter_app`.
- **Changed:** a requirement gains `approval`, as its own route answers it.
- **UI:** the requirement page shows the review and its decisions with
  Approve and Reject for an approver, Ask for review for an editor; the
  project's settings hold the rules; `/reviews` is the inbox, with a count in
  the top bar.

## #36 Suspect links

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/suspects` | view | `{links, total, limit, offset}` |
| `POST /links/{linkID}/clear` | edit on either end | `{link}`; 403; 409 `revision_moved_on`; 422 |
| `GET /links/{linkID}/clearances` | view of both ends | `{clearances}`, newest first |
| `POST /armature-links/{linkID}/clear` | edit | `{link}`; 403; 409 `revision_moved_on`; 422 |

- **The rule.** A trace link remembers the revision of each end it was made
  or last confirmed against (`sourceRevision`, `targetRevision`, #18). Its
  type's `suspectEnd` says which end it watches: `target`, `source`, `both` or
  `none`. The link is suspect while a watched end has a newer revision than
  the one remembered. It is computed, in the view `trace_link_state`, and
  never stored; a link of a type watching nothing is never suspect, and one
  whose end is in the trash is not either until it comes back.
- **The upstream end** is the watched one; the downstream end is the other,
  whose people look again. With `both`, either end's revision turns it.
- **Clearing** confirms the link against both ends as the caller read them,
  with a comment of 1 to 500 characters: a revision newer than the one named
  is 409 `revision_moved_on`, so nobody confirms what they did not see.
  Whoever may edit either end clears. It moves the remembered revisions on
  and records who, when, against which revisions and why in
  `trace_link_clearance`. The client shows the watched end's diff (#16)
  from the remembered revision to the current one before the button.
- **Armature links** take the same rule against the requirement alone: they
  remember the requirement's revision when made or confirmed
  (`armature_issue_link.reviewed_rev`, existing links taking their
  requirement's current one), and are suspect while the requirement has a
  newer one; clearing is the requirement's editors'.
- **Notifications:** the owner and watchers of each downstream end are told
  `suspect` when a revision turns its link (#34).
- **In the database:** the remembered revisions change only by clearing,
  through a function that checks the clearer may edit an end and names
  current revisions; the clearances are append-only. Each is proven through
  SQL as `exciter_app`.
- **Changed:** a link answered by `GET /requirements/{reqKey}/links` gains
  `suspect: [{end, reviewed, current}]`, empty when it is not; an Armature
  link gains `suspect` and `reviewedRevision`; a grid row and a
  specification entry gain `suspectLinks`, the count, and the grid takes
  `suspect=true` to keep only requirements with one.
- **UI:** a Suspect badge on links in the links panel, the specification
  view and the grid; a Suspect list per project; Clear opens the diff and
  asks for the comment.

## #37 Baselines

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/baselines` | view | `{baselines}`, newest first |
| `POST /projects/{projectKey}/baselines` | administer | 201 `{baseline}`; 403; 409 `baseline_name_taken`; 422 |
| `GET /baselines/{baselineID}` | view | `{baseline}` |
| `PATCH /baselines/{baselineID}` | administer | `{baseline}`; 403; 409 `baseline_locked`; 422 |
| `DELETE /baselines/{baselineID}` | administer | 204; 403; 409 `baseline_locked` |
| `GET /baselines/{baselineID}/spec` | view | `{entries, total, limit, offset}` |
| `GET /baselines/{baselineID}/compare?with=` | view | `{comparison}`; 422 |

- **Taking one.** A baseline is of the whole project, or of one section and
  everything below it (`rootKey`). It records, for each live requirement, the
  revision, the parent, the rank and the status, as `baseline_item`; every
  trace link between two of them with its type, as `baseline_link`; and every
  Armature link of them with its relation, as `baseline_external_link`.
  Content is never copied: an item names `requirement_revision`. It is one
  `INSERT ... SELECT` per table in a single REPEATABLE READ transaction, so a
  change committed meanwhile is wholly in or wholly out. Names are unique per
  project, at most 100 characters.
- **Never changed.** Items, links and external links are insert-only for the
  app role, and the trigger refuses any change after the baseline's own
  transaction. Locking (`locked: true`) is for good: a locked baseline keeps
  its name and description and is never deleted. Deleting an unlocked one
  takes its rows.
- **Reading one** as a specification answers each requirement at the revision
  it holds, in the baseline's outline, numbered as it was.
- **Comparing** with another baseline of the project, or with now (`with=now`,
  the default). Each requirement on either side is `added`, `removed` or
  `changed`; a changed one says whether it was `revised` (another revision),
  `moved` (another parent or place) and `statusChanged`, with both sides'
  revision, title, outline and status, and is left out when nothing differs.
  Links and external links are listed as added or removed. A requirement's
  own diff is #16's, `GET /requirements/{reqKey}/diff?from=&to=` with the two
  revisions; a requirement purged since is listed by its key alone.
- **UI:** Baselines in the project, Take baseline for administrators, a
  baseline read like the specification, and Compare with another or now,
  each requirement opening its diff.

## #38 Traceability matrix and coverage

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/coverage-rules` | view | `{rules}` |
| `PUT /projects/{projectKey}/coverage-rules` | administer | `{rule}`; 403, 422 |
| `DELETE /coverage-rules/{ruleID}` | administer | 204; 403 |
| `GET /projects/{projectKey}/coverage` | view | `{summary, rows, total, limit, offset}`; 422 |
| `GET /projects/{projectKey}/matrix` | view | `{matrix}`; 422 |
| `GET /projects/{projectKey}/matrix.csv` | view | a CSV file; 422 |

- **Rules.** As approval rules, one per type and one for every type without
  its own, each saying which of `decomposed`, `implemented` and `verified`
  apply. Without any rule nothing applies, and every level is `na`. Sections
  are never counted.
- **The levels** of one requirement in one dimension are `none`, `partial`,
  `full`, or `na` when its rule does not ask:

  | Dimension | Counts | Full | Partial |
  |---|---|---|---|
  | decomposed | trace links of a type that counts `decomposition`, where the requirement is the target | at least one, none suspect | some, any suspect |
  | implemented | Armature links `implements`, neither broken nor detached | at least one issue done, none suspect | linked, none done, or any suspect |
  | verified | trace links of a type that counts `verification` where it is the target, and Armature links `verifies` | at least one, none suspect, every Armature one done | some, any suspect, or an Armature one not done |

  No such link is `none`. The status category comes from the link, stored
  for everyone (#27), so coverage reads no issue as the viewer, and a
  requirement or link the viewer may not see is not counted.
- **The report** narrows the project's requirements like the grid (#19) and
  answers the totals of each dimension over the requirements it applies to,
  and each requirement's levels, a page at a time.
- **The matrix** pages the requirements the filter keeps as rows, against the
  requirements they link to (`columns=requirements`, of `columnProject`, the
  same project by default) or the Armature issues they are traced to
  (`columns=issues`). A cell lists every link between its row and column, its
  type's name or the Armature relation, and whether it is suspect. At most
  `trace.MatrixMaxColumns`, 500, columns are answered; more is 422 asking for
  a narrower filter. `matrix.csv` is the same matrix for every row the filter
  keeps, up to the export's limit (#10), with a column per column and the
  link names in its cells.
- **Cached** in Valkey per project and filter for a minute, and cleared by a
  revision, a link made, removed or cleared, an Armature link's status, and a
  rule saved.
- **UI:** Coverage in the project, with the three bars and a list to filter
  by level, and the matrix with its CSV download.

## What is left for later

- Inline comments on a statement's words (Stator's #23): a requirement is
  revised as a whole, so a comment on a passage would need its own anchoring
  across revisions. Not in M3.
- Reactions to comments, and a push channel for the bell.
- Baselines across projects.
