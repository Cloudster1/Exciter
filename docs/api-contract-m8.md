# API contract for M8: Review sessions and control

M8 puts deliberate steps between a specification and its release:
- a baseline is signed by the people who release it, and a signed one never
  changes
- a requirement in a released status, or locked by hand, takes no new
  revision, move, link or trash
- fields are required from a status on, choices depend on other choices,
  fields are changed only by the roles allowed to, and transitions ask for
  trace
- a section, a baseline or a query is reviewed in one session, in rounds,
  and closed with a report

The epic is #215; this file is #216. The route table in
`backend/internal/httpapi/operations.go` and `operations_m8.go` is the source
of truth for paths, fields and types, and this file says what they mean.
Operations it adds carry `pending: true` and answer 501 until their issue is
built. Parameters and fields it adds to operations that exist arrive with
their issue.

All paths are under `/api/v1`. Every operation needs somebody signed in.
"view", "edit", "approve" and "administer" are the project rights of #12. A
project or requirement the caller may not view is 404, never 403, and a
change in an archived project is 409 `project_archived`. A refusal is a
sentence that says what to do, as in M1.

## Migrations, packages and settings

- Migration ranges, in build order:
  - 00900 to 00919 for signed baselines and locks (#205)
  - 00920 to 00939 for field rules (#206)
  - 00940 to 00959 for review sessions (#204)
- Packages:
  - `internal/baseline` gains signing (#205). The proof of identity of #46
    moves from `internal/review/signing.go` to `internal/signing`, which
    reviews and baselines both call.
  - `internal/attribute` gains the field rules (#206), and
    `internal/workflow/rules.go` the new rule types.
  - `internal/review` gains sessions (#204), and `internal/specdoc` and
    `internal/docx` the session report.
- Settings: none. No new environment variables or installation settings.
- Limits:
  - `baseline.MaxSigners` = 20: people and groups one signing rule names.
  - `requirement.MaxLockReasonLength` = 500.
  - `attribute.MaxPermits` = 20: role and status pairs on one field.
  - `review.MaxSessionItems` = 500: requirements one session holds.
  - `review.MaxSessionParticipants` = 50: people and groups one session
    names.
  - `review.MaxSessionApprovers` = 50: people a session's approvers reach,
    groups counted by their members.
  - `review.MaxSessionRounds` = 20.
  - `review.MaxSessionTitleLength` = 100, and the description
    `review.MaxCommentLength` as a review's comment.
  - `review.MaxDeadline` = 365 days ahead; a deadline is at least an hour
    ahead when it is set.
  - `review.ReminderLead` = 24 hours: when the reminder goes out.
- **Decided** (`docs/decisions.md`, 2026-10-17):
  - a session's approvers replace the approval rule for its reviews
  - a hand lock is apart from a status lock
  - the new transition conditions offer the transition, then refuse it
    with a sentence
  - a password signature is proven by the api, and the database proves
    the proof is fresh, as in #46

## #205 Signed baselines and locks

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/baseline-signing` | view | `{rule}`, null without one |
| `PUT /projects/{projectKey}/baseline-signing` | administer | `{rule}`; 422 |
| `DELETE /projects/{projectKey}/baseline-signing` | administer | 204 |
| `GET /baselines/{baselineID}/signatures` | view | `{signatures}` |
| `POST /baselines/{baselineID}/signatures` | a signer, signed in | `{baseline}`; 403 `session_only`, `not_a_signer`; 409 `no_signing_rule`, `already_signed`, `signature_spent`, `baseline_changed`; 422 on `password`; 429 `too_many_attempts` |
| `POST /requirements/{reqKey}/lock` | administer | `{requirement}`; 409 `section_not_locked`; 422 |
| `DELETE /requirements/{reqKey}/lock` | administer | `{requirement}`; 409 `not_locked_by_hand` |

### Signing

- **The signing rule** is the project's one: `{required, signers}`.
  - `signers` are people or groups, as a review's approvers are
    (`review.Subject`), at least one and at most `MaxSigners`.
  - `required` is how many signatures release a baseline, 1 to
    `review.MaxRequired`.
  - Saving and removing it are audited as `baseline_signing.saved` and
    `baseline_signing.removed`, and each signature as `baseline.signed`.
  - Without a rule, a baseline is not signed: signing answers 409
    `no_signing_rule`, "Nobody signs this project's baselines yet. Ask a
    project administrator to name who signs them."
- **The first signature freezes the rule** on the baseline, as a review
  freezes its approvers: the signers and the number it needs are the rule's
  as it stood then, so a later change to the rule does not change who signs
  a baseline already being signed.
- **Who signs.** Somebody the frozen signers name, directly or through a
  group, who may view the project; anybody else is 403 `not_a_signer`. Each
  person signs a baseline once; a second time is 409 `already_signed`. A
  token never signs (403 `session_only`), as in #46.
- **Proving who one is** is #46's: the request takes `password`, or the
  `reauthentication` a fresh sign-in at the identity provider left, through
  `GET /auth/oidc/reauthenticate?next=`. Its refusals are #46's: 422 on
  `password`, 409 `signature_spent`, 429 `too_many_attempts`.
- **The meaning** is written by the server in the signer's language and
  kept as shown: "I release the baseline <name> of <project> as the agreed
  state of its requirements." The input also takes `meaning`, the sentence
  as the dialog showed it, and a sentence that differs is 409
  `baseline_changed`: the dialog reads the baseline again.
- **A signature keeps** the meaning, the signer, how and when they proved
  who they are, the session, its `reauthentication` and `contentHash`. It
  never changes.
- **The content hash** is the SHA-256, in lowercase hex, of the baseline's
  content in a fixed form, one line per row, each ending in `\n`:
  - `baseline <id> <project id> <root id or ->`, by ids, since a project's
    key can change
  - per item, in path order:
    `item <requirement id> <revision> <revision hash or -> <path>`, the path
    its outline numbers joined by `.`
  - per trace link, ordered by type id, source id and target id:
    `link <type id> <source id> <target id>`
  - per Armature link, then per Stator link, ordered by their ids:
    `external <id>` and `page <id>`

  The SQL function `baseline_content_hash(id)` computes it, and the guard
  recomputes it. `baseline.ContentHash` computes it in Go from the same
  rows, for the unit test and for the report.
- **The baseline gains** `signing: {required, signers, signatures, signed,
  frozen, canSign, meaning}`, null while nobody signs the project's baselines
  and no rule is frozen on it.
  - Until the first signature it shows the project's rule, `frozen` false;
    from then on the rule frozen on the baseline.
  - `signatures` counts those given.
  - `signed` is true once it reaches `required`.
  - `canSign` says the caller may sign it now, in a session, and `meaning`
    is then the sentence they sign, in their language, empty otherwise.
  - The list of signatures is `GET /baselines/{baselineID}/signatures`,
    each `{signerName, meaning, method, authenticatedAt, signedAt,
    contentHash}`.
- **A signed baseline** is one with any signature:
  - it is locked, and the database sets `locked_at` with the first
    signature
  - a rename or a delete is 409 `baseline_signed`, "This baseline is
    signed and keeps its name. Take a new baseline to record a later
    state."
  - the list, its page, its Word and PDF covers, its Markdown and its
    matrix CSV and workbook say "Signed" with the number of signatures, or
    "Signing, n of m" while it is short of them
- **The database proves it too.** `baseline_signature_guard` refuses a
  signature:
  - not given by a frozen signer in their own name
  - without a fresh, unused re-authentication of the same session, which it
    spends
  - whose `contentHash` is not `baseline_content_hash` of the baseline
  - a second one by the same signer

  A password re-authentication is written by the api once it has checked
  the password, as in #46; the database proves it is fresh, single-use and
  the signer's own session's, not the password itself.

### Locks

- **A status locks its requirements** when its `locks` is true.
  `workflow.Status` and `StatusInput` gain `locks`; setting it is a change of
  the status, for the project's administrators, audited as `status.locks_set`.
- **A hand lock** is set by `POST /requirements/{reqKey}/lock` with
  `{reason}`, which changes the reason of a hand lock already there, and
  cleared by `DELETE`. Both are audited, as `requirement.locked` and
  `requirement.unlocked`, and logged in the requirement's activity. A
  section is not locked by hand: 409 `section_not_locked`.
- **Locked** is either. Unlocking by hand clears only the hand lock; a
  requirement in a locking status leaves it by a transition, which needs no
  more than its own conditions.
- **The requirement gains** `lock`, null when it is not locked, otherwise
  `{by: "status" | "hand", status, reason, lockedByName, lockedAt}`; a
  requirement locked both ways answers `hand`. A locked requirement answers
  `can.edit` and `can.delete` false and `can.link` false.
- **What a lock refuses**, each 409 `requirement_locked` with a sentence
  that names why: "CHK-R4 is locked in the status Released. Take it out of
  Released to change it." or "CHK-R4 is locked by Ada Lovelace: <reason>.
  Ask a project administrator to unlock it."
  - a new revision, from any writer: a save, a restore, an import, a
    reuse take, a move to another project
  - a move: another parent or another place among its siblings
  - a trace link made or removed where it is the source; a link to it from
    another requirement is that requirement's change and is not refused
  - the trash
  - owner, priority and labels are not normative and stay open
- **What a locked requirement still takes**: comments, reviews and their
  decisions, transitions, watching, a suspect link confirmed, Armature and
  Stator links, and a baseline of it.
- **One function decides it.** `requirement_locked(id)` is read by the
  triggers on `requirement_revision`, on `requirement` for the parent, rank
  and trash, and on `trace_link`, each raising `requirement_locked`, so the
  lock holds for every writer, the MCP tools and the imports included.
- **UI.** The requirement page and its document entry show a banner saying
  who or which status locked it and why; the editor, move and trash are
  not offered; the status settings gain Locks requirements; an
  administrator locks and unlocks from the requirement's menu.

- **As built** (#205):
  - The proof of identity of #46 is `internal/signing`: `Prove` checks the
    password before the transaction, `Spend` finds or records the
    re-authentication in it. `review` keeps its error values and
    `WithSigner` over it, so its refusals and handlers read as before.
  - `baseline_content_hash` hashes in the database for the guard;
    `baseline.ContentHash` hashes in Go the rows `baseline_content_rows`
    hands a reader of the project, who may not read a trashed requirement's
    revisions, so both hash the same rows.
  - The first signature freezes the rule into `baseline_signing` and
    `baseline_signer` and sets `locked_at`, all in the guard, so no writer
    signs a baseline its project's rule does not name.
  - The lock is three triggers, `requirement_revision_unlocked`,
    `requirement_row_unlocked` and `trace_link_unlocked`, over one function,
    `requirement_lock_sentence`, whose sentence is the refusal's message:
    the API, the CSV and ReqIF imports and the MCP tools say it as it is.
    A first revision is a requirement's making and is never refused, so a
    status that locks may be where requirements start.
  - The row trigger compares what it guards, since a trigger on columns
    fires for any of them set, changed or not.
  - A hand lock is written only by `requirement_lock` and
    `requirement_unlock`, which hold it to the project's administrators; the
    app role holds no grant on its columns. Locking needs a reason.
  - A requirement locked answers `can.edit`, `can.delete` and `can.link`
    false; linking to it reads the reader's edit right alone, so a link to
    it from another requirement goes on.
  - The signing rule's signers are picked as an approval rule's, and the
    project's settings show it under Approval, beside the approval rules.

## #206 Field rules

An attribute's rules are settings of its definition, on the operations that
exist; the fixed fields' are two new operations. The editor reads both with
what it reads already.

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/field-rules` | view | `{rules}` |
| `PUT /projects/{projectKey}/field-rules/{field}` | administer | `{rule}`; 422 |

- **Fields** are the attributes, by their ids, and the fixed fields
  `title`, `statement`, `rationale`, `type`, `owner` and `priority`. An
  attribute's rules are fields of its definition; a fixed field's are the
  project's `field-rules`, one per field, in the same shape. Saving either
  is audited as `field_rule.saved`.
- **Required from a status.** `requiredFrom` is a status id, or null.
  - The field is required in that status and in every status after it in
    the project's status order (`req_status.position`).
  - A save of a requirement in such a status without a value is 422 on the
    field, "Give <field> a value; requirements in <status> need it."
  - A transition into such a status runs the required check of
    `validator.required_attributes` for it, whether or not that validator
    is on the transition, with the same sentence.
  - `title` and `statement` are required always and take no
    `requiredFrom`; the attribute's own `required` stays, and means from
    the first status on.
- **Dependent choices.** A select or multi-select attribute takes
  `dependsOn: {attributeId, choices}`.
  - `attributeId` is another select attribute of the project.
  - `choices` maps each of its option ids to the option ids of this one
    that it allows; an option of the other not named allows every option.
  - A value outside the allowed options is 422 on the field, "<option> is
    not a choice of <field> when <other> is <option>. Pick one of: <list>."
  - The editor and the grid offer only the allowed options and clear a
    value the other's change disallows; the CSV and ReqIF imports report
    the row; the API refuses it; and the database refuses it too
    (`attribute_value_choice`).
  - A dependency on itself, on an attribute that depends on it in turn,
    or across kinds other than select is 422 on `dependsOn`.
- **Who changes a field.** `editableBy` is a list of permits
  `{roles, statuses}`, at most `MaxPermits`.
  - Empty, the field is changed by whoever may edit the requirement.
  - Otherwise a change of the field needs one of the roles of a permit
    whose statuses hold the requirement's status; an empty `statuses` is
    every status. The roles are the project roles of #12, held as the
    caller's grants give them.
  - A change by somebody else is 422 on the field, "Only <roles> change
    <field> in <status>."
  - The project gains `roles`, the reader's project roles, from which the
    editor, the requirement's page and the grid work out what the reader may
    not change and show it read-only with why; the server and the database
    hold the same rules.
  - The database refuses it too (`field_not_yours`), comparing the
    revision or the row before and after, as `current_actor_id()` holds
    the roles of `perm_project_roles`, so a SECURITY DEFINER function that
    writes for somebody, such as a transition's post-function setting the
    owner, is held to the person it writes for.
- **Conditions on trace.** Four rule types join the registry of
  `internal/workflow/rules.go`. They are validators, so a transition with
  one stays offered and is refused when taken, and the designer lists them
  with the conditions:
  - `validator.links_exist` `{linkTypeId, direction: "outgoing" |
    "incoming"}`: at least one live link of the type; "CHK-R4 has no
    verifies link yet. Link it before moving it to Approved."
  - `validator.no_suspect`: no suspect trace, Armature or Stator link;
    "CHK-R4 has 2 suspect links. Confirm or change them first."
  - `validator.coverage_full` `{dimension: "decomposed" | "implemented" |
    "verified"}`: coverage is full in that dimension, as
    `GET /projects/{projectKey}/coverage` judges it; "CHK-R4 is partly
    verified. Cover it fully first."
  - `validator.threads_resolved`: no open comment thread; "CHK-R4 has 3
    open comment threads. Resolve them first."
  - They count the links, suspects and threads the taker sees, and coverage
    as `GET /projects/{projectKey}/coverage` judges it for them.
  - `workflow.OptionKinds` gain `linkType`, `direction` and `dimension`.
- **Refusals.** A refused transition answers 422 `validation_failed` with
  the rule's sentence on the field `transition`; the bulk transition of
  #169 answers it per requirement, through `refusalOf`, so the document
  says for each requirement what is missing.
- **UI.** The attribute form gains Required from, Depends on, with a table
  of the choices, and Who may change it; the project settings gain a Fields
  tab for the fixed fields; a read-only field says why; the rule editor
  picks a link type, a direction and a dimension.
- **As built** (#206):
  - The rules are `attribute_def.required_from`, `depends_on`, `choices` and
    `editable_by`, and `field_rule` for the fixed fields; `attribute.Rules`
    weighs them for a save, a creation and a transition alike, with the
    project's statuses in order.
  - A save is checked field by field: what it changes against the permits,
    then what its status asks for of the requirement as it would stand. A
    creation is checked against its first status, and a transition against
    its target, before it is taken.
  - Dependent choices are checked only where the dependent attribute or the
    one it depends on changes, in the service and in
    `requirement_choices_hold`, so values given before a rule refuse nothing
    until one of them moves. The editor and the grid offer only the choices
    allowed, and the editor clears a choice the other's change disallows.
  - `requirement_fields_permitted` holds the permits against
    `perm_project_roles` of `current_actor_id()`, in the status the
    requirement is in, for every writer that acts for somebody; the
    worker's own writes act for nobody and are not held.
  - A field's permits answer 422 on the field, not 403, as every other
    sentence on a field does; the database's own refusal is 403
    `field_not_yours`.
  - The project answers `roles` rather than each requirement a map of its
    fields, so the grid reads one project for all its rows.
  - The trace conditions read a requirement's links over every link type,
    so a type it has no link of is named too; coverage comes from
    `trace.CoverageJudge`, given to the requirement service, since `trace`
    reads requirements itself.
  - Validators now take their rule's config, as conditions did.
  - An attribute's form sends the rules only where they changed.

## #204 Review sessions

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/review-sessions` | view | `{sessions, total, limit, offset}` |
| `POST /projects/{projectKey}/review-sessions` | edit | 201 `{session}`; 409 `project_archived`; 422 |
| `GET /review-sessions/{sessionID}` | view | `{session}` |
| `PATCH /review-sessions/{sessionID}` | the moderator | `{session}`; 409 `session_closed`; 422 |
| `POST /review-sessions/{sessionID}/rounds` | the moderator | `{session}`; 409 `session_closed`, `nothing_revised`, `too_many_rounds` |
| `POST /review-sessions/{sessionID}/close` | the moderator | `{session}`; 409 `session_closed`, `baseline_name_taken`; 422 |
| `GET /review-sessions/{sessionID}/report` | view | `{report}` |
| `GET /review-sessions/{sessionID}/report.docx` | view | the report as Word |
| `GET /review-sessions/{sessionID}/report.pdf` | view | the report as PDF |
| `GET /reviews/sessions` | signed in | `{sessions, total, limit, offset}` |

- **The moderator** is whoever opened the session, or any of the project's
  administrators.
- **Opening** takes `{title, description, scope, since, participants,
  deadline, quorum}`, all in one transaction: either the session opens with
  every review, or nothing is written.
  - `scope` is one of:
    - `{kind: "section", rootKey}`: the section and everything below it
    - `{kind: "baseline", baselineId}`: the requirements the baseline
      holds that are still in the project
    - `{kind: "query", query}`: a query in the language of #47, as
      `GET /projects/{projectKey}/requirements?q=` reads it
  - `since`, optional, keeps only what changed: `{base: "approved"}` since
    each requirement's last approval, or `{base: "baseline", baselineId}`
    since a baseline, as `GET /projects/{projectKey}/spec?changes=` decides
    it.
  - Sections are never reviewed; they give the session its outline.
  - Each requirement is pinned at its current revision. A baseline's
    revision is the comparison base for it in the first round, not what is
    reviewed: an approval is always of the current revision (#35).
  - A requirement with a pending review of its own is left out, and the
    session lists it under `excluded` with `review_pending`; so is a
    requirement in the trash.
  - Nothing left to review is 422 on `scope`, "Nothing in this scope needs
    a review. Widen it, or drop since."; more than `MaxSessionItems` is 422
    on `scope`.
- **Participants** are `[{subjectType, userId | groupId, role: "reviewer"
  | "approver"}]`, at least one approver.
  - Approvers decide; reviewers read and comment, and their decisions are
    not asked.
  - A group's members take part as the group's members are when they act,
    as review approvers do.
  - Somebody who may not view the project is 422 on `participants`, so is
    an approver without the approve right.
- **The quorum** is the share of approvers each requirement needs, in
  percent, 1 to 100, 100 by default. Each review requires the ceiling of
  that share of the people the approvers reach when the session opens,
  at least 1, and at most `MaxSessionApprovers`.
- **Each requirement's review is a review of #35.** Its approvers are the
  session's approvers, frozen in `review_subject`, and its `required` the
  quorum's; the type's approval rule does not apply. So a decision, its
  signature where the project signs approvals (#46), a rejection that
  closes the review, `requirement_approved` and the "Approved" condition
  are as they are.
  - `review_follows_rule` lets a review that a session item made follow its
    session instead of its rule.
  - `requirement_review.required` may be up to `MaxSessionApprovers` for
    those reviews; a rule's stays at most `review.MaxRequired`.
  - A review carries `session`, `{id, title, round}`, or null.
- **Deciding** is `POST /reviews/{reviewID}/decisions` as it is. The
  database refuses a decision on a session's review unless the session is
  open and the review is of the current round's pinned revision of its
  item (`review_session_decision`). Somebody who does not take part is
  refused already, as the review does not name them.
- **Notifications.** `notification.requirement_id` may be null for the new
  kinds, which name `session_id` instead:
  - `session_invited`: each participant, when it opens
  - `session_round`: each participant, when a round starts
  - `session_reminder`: each participant with something still to decide,
    or for a reviewer nothing commented, once, `ReminderLead` before the
    deadline
  - `session_closed`: each participant, with its outcome
  - each is in-app and by mail as the person's preferences say, with a new
    preference per kind. The single reviews of a session send no
    `review_requested` of their own.
  - The reminder is sent by `review.Reminder`, a loop of the worker beside
    `notify.Digester`, which marks the session reminded so it is sent once.
- **Progress.** The session answers, beside its `participants` as named:
  - `items`: per requirement `{requirement, outline, revision, round,
    review, openThreads}`: the pinned revision, the round it was pinned in,
    its review with every decision, and its open comment threads
  - `progress`: per person `{person, role, decided, commented, of}`,
    groups counted by the members who acted
  - `counts`: `{approved, rejected, pending, total}`
- **A new round** takes `{comment}`. Each requirement revised since its
  pin is pinned anew at its current revision, with a new review; the
  others keep their review and its decisions, approved, rejected or still
  pending. Nothing revised is 409 `nothing_revised`. Each round compares
  with the last: the specification with `session=<id>` and
  `changes=round` marks what changed since the revision the previous round
  pinned.
- **Editing** with `PATCH` changes the title, the description and the
  deadline; participants and the scope are fixed once it opens.
- **Closing** takes `{baseline}`, optional `{name, description}`.
  - Every review of the session still pending is withdrawn.
  - With `baseline`, it takes a baseline of the requirements whose current
    revision the session approved, and the sections above them, which the
    session names as its baseline (`baseline.session_id`). The moderator
    takes it through `review_session_close`, so an editor who moderates
    needs no administer right for it. A name already taken is 409
    `baseline_name_taken`.
  - A closed session is read-only: 409 `session_closed` for any change.
  - Opening, a new round and closing are audited as `review_session.opened`,
    `review_session.round_started` and `review_session.closed`.
- **The report** is the session as closed, or as it stands while open:
  title, scope, moderator, the dates, the participants and what each
  decided, every requirement with its decisions per round and its open
  comment threads, the rounds with when they started, and the baseline it
  took. `report.docx` writes it with `internal/docx`; `report.pdf` prints
  the web's `/print/sessions/$sessionId` through the render service, as the
  document export of #45 does. Downloading is audited as
  `specification.exported`, as the document is.
- **The inbox.** `GET /reviews/sessions` lists the open sessions the caller
  takes part in, across projects, nearest deadline first, each with what
  is still the caller's to do; `/reviews` lists them above the single
  reviews. The project lists its sessions, open first, with `state=open` or
  `closed` to keep one.
- **The document in review mode.** `GET /projects/{projectKey}/spec` gains
  `session`, a session of the project: the session's requirements, at the
  revision pinned now, and the sections above them; each entry's `review`
  is its session review. With `waiting=true` it keeps what waits for the
  caller.
- **MCP.** `list_review_sessions` and `get_review_session` read; no tool
  opens, decides or closes a session, as no tool decides a review (#182).
- **UI.** Open a session from the document's selection, a section's menu,
  a baseline's page or the requirements' query; the dialog names the
  participants, deadline and quorum; the session page shows progress per
  requirement and per participant, starts a round, closes with a baseline
  and downloads the report; review mode reads the session.
- **As built** (#204):
  - A session's reviews are asked only by `review_session_pin`, a SECURITY
    DEFINER function the moderator calls: it pins the current revision, asks
    the review in the moderator's name, freezes the session's approvers on it
    and sets `required` from the quorum, not counting the revision's author,
    who never approves it. `requirement_review_not_in_session` keeps the app
    role from asking one itself.
  - `review_decidable` learned that a closed session takes no decision; a
    decision on a review the session does not name was refused already, and
    a review of an older pin was superseded by the revision that made it
    older.
  - Opening, a round and closing each happen in one transaction; a round
    pins anew a requirement revised since its pin, out of the trash and
    without a review of its own waiting (`pinAgain`).
  - A reviewer needs the comment right and an approver the approve right in
    the project, so everybody named can do what the session asks of them.
  - The closing baseline is taken by `baseline.TakeOfSession` in the closing
    transaction, of the requirements approved at their revision now and the
    sections above them; `baseline_takers` lets the moderator take it while
    `review_session_closing` holds.
  - Notifications name the session rather than a requirement
    (`notification.session_id`), are read by whoever may read its project,
    and lead to `/p/{projectKey}/sessions/{sessionId}`. The reminder is the
    worker's `review.Reminder`, which runs whether or not mail is on and
    tells only approvers with a decision still to give.
  - The specification's `changes=round` compares each requirement with the
    revision its previous round pinned, or with its last approval outside
    the session when it was pinned once.
  - The Word report is written with `internal/docx`'s own blocks
    (`docx.ReportBytes`); the PDF prints `/print/sessions/{sessionId}`
    through the render service, as a document's does (`Server.printed`).
  - The web opens a session from a section of the document, a baseline's
    page and the project's sessions, where a query is given too.

## What is left for later

- A signature on a whole review session, as one act for every requirement:
  a decision stays per requirement, signed one at a time.
- Moving a section that holds a locked requirement is not refused: the lock
  holds the requirement's own place among its siblings, not its ancestors'.
- Reminders at times of the moderator's choosing, beyond the one before the
  deadline.
