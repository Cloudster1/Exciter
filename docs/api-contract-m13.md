# API contract for M13: Risk management

M13 keeps the risk file beside the specification:
- a requirement type marked as risks gives its requirements a rating before
  and after mitigation, on the scales and the levels a project's
  administrators set, with a level that follows from a matrix or a
  priority number
- a requirement mitigates a risk through a link, and a mitigation is in
  effect once its requirement is implemented and verified; the FMEA table
  and the risk matrix show what is left and where
- risks appear in the trace graph, the chain report and baselines, a risk
  report goes out as PDF and Word, and two project templates set up a
  medical device and a functional safety project

The epic is #203; this file is #258. The route table in
`backend/internal/httpapi/operations.go` and `operations_m13.go` is the
source of truth for paths, fields and types, and this file says what they
mean. Operations it adds carry `pending: true` and answer 501 until their
issue is built. Parameters and fields it adds to operations that exist
arrive with their issue.

All paths are under `/api/v1`. Every operation needs somebody signed in or a
token. "view", "edit" and "administer" are the project rights of #12. A
project or requirement the caller may not view is 404, never 403, and a
change in an archived project is 409 `project_archived`. A refusal is a
sentence that says what to do, as in M1.

## Migrations, packages and settings

- Migration ranges, in build order:
  - 01260 to 01279 for risks and their ratings (#259)
  - 01280 to 01299 for mitigations, the FMEA table and the risk matrix (#260)
  - 01300 to 01319 for risks in traceability, baselines, the report and the
    templates (#261)
- Packages:
  - `internal/requirement` gains a risk's rating (#259), checked and hashed
    in `risk.go`, as it carries a test case's steps.
  - `internal/risk` (#259): the project's risk scheme, the levels it gives,
    and from #260 mitigations, the FMEA rows, the matrix and from #261 the
    report. It sits above `internal/requirement`, `internal/search` and
    `internal/trace`, which it reads; nothing below reads it, but for
    `internal/trace` reading a risk's mark through SQL (#261).
- Settings: none in the environment. A project keeps its risk scheme.
- Limits:
  - `risk.MaxScaleValue` = 10: a scale has the values 1 to 10 at most, each
    named.
  - `risk.MaxLevels` = 10 levels a scheme.
  - `risk.MaxNameLength` = 100, `risk.MaxLabelLength` = 50 and
    `risk.MaxDescriptionLength` = 500 characters.
  - `risk.MaxRows` = 200 risks a page of the FMEA table.
- **Decided** (`docs/decisions.md`, 2026-10-19):
  - a risk is a requirement of a risk type, not a kind or a table of its own
  - the scheme is held in tables of the project, and a rating as numbers
  - a level is always worked out from the scheme, never stored with a
    rating
  - the rating after mitigation counts only once every mitigation is in
    effect
  - a coverage level the project does not ask for counts as met for a
    mitigation

## #259 Risks and their ratings

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/types`, `PATCH .../types/{typeID}` | administer | `role` may be `risk` |
| `GET /projects/{projectKey}/risk-scheme` | view | `{scheme}`, null until one is set |
| `PUT /projects/{projectKey}/risk-scheme` | administer | `{scheme}`; 409; 422 |
| `POST /projects/{projectKey}/requirements`, `PATCH /requirements/{reqKey}` | edit | `risk` |
| `GET /requirements/{reqKey}`, its revisions and history | view | `risk` with its levels; a rating diff |

### The role of a type

- A requirement type's `role` may be `risk`, beside `requirement` and
  `testCase` (#250). Only a type's role makes its requirements risks; they
  stay requirements in every other way, with a key `KEY-R<n>`, revisions,
  reviews, status, attributes, labels, links and a place in the tree.
- **Kinds** of risk are types: a medical device project has Hazard,
  Hazardous situation and Harm, a functional safety project Failure mode.
  Their effects and causes are attributes, and how one kind leads to
  another is a link.
- A type keeps `risk` while any of its requirements, trashed ones included,
  holds a rating or is mitigated; turning it back is 409 `type_role_held`,
  which names how many.

### The risk scheme

A project's administrators set it whole:
- **Scales**: `severity`, `probability` and, when `detection` is on,
  `detection`. Each has a label the project uses for it (for example
  "Occurrence" for probability) and its values from 1 up, at most
  `MaxScaleValue`, each with a name and a description. A higher value is
  worse: more severe, more likely, harder to detect.
- **Levels**: in order, from the least to the most severe, each with a name,
  a colour and whether a risk at that level is **acceptable**. At least one,
  at most `MaxLevels`.
- **The method**:
  - `matrix`: every cell of severity and probability names a level;
  - `rpn`: the priority number is the product of severity, probability and,
    when on, detection, and each level from the second up names the
    `minRpn` from which it applies.
- A scheme is complete or refused: every cell of a matrix filled, the
  thresholds of a priority number rising.

### Ratings

- A risk is rated **before** and **after** mitigation; each side gives the
  value of each scale, any of them null while unknown. A side with a value
  missing has no level.
- Only a requirement, not a section, of a risk type takes a rating, and
  only with values on its project's scales; anything else is refused as
  422 on the field.
- **The level** of a side follows from the scheme: the matrix's cell, or the
  highest level whose `minRpn` the priority number reaches. It is never
  stored with the rating, so a scheme changed changes the levels of every
  risk at once; a baseline keeps the level it had (#261).
- **Changing the type** of a risk to one that is not a risk type drops its
  rating, which writes a revision.

### Revisions and hashes

- A rating is part of every revision, of the normative content and of its
  hash, so a change to a rating writes a revision, a signature covers it
  and a lock (#205) keeps it.
- The hash leaves it out when it is empty, so every requirement without one
  keeps the hash it has, and every baseline its signature.
- History shows each value changed, before and after.

### What shows them

The requirement page, the print view and the document (#45), its Word and
Markdown exports, and `get_requirement` show both sides with their values,
named, the priority number and the level.

### CSV and ReqIF

- **CSV** (#23): columns `Severity`, `Probability`, `Detection`,
  `Severity after`, `Probability after` and `Detection after`, the values as
  numbers; an import reads them back for risks and refuses them on
  anything else.
- **ReqIF** (#44): `exciter.risk`, a string `before S P D; after S P D`
  with `-` for a value unknown, read back from Exciter's own definition.

### Queries

The query language gains `risk` (whether a requirement is one),
`severity`, `probability`, `detection` and `rpn` of the rating before,
`severityAfter`, `probabilityAfter`, `detectionAfter` and `rpnAfter`, and
`riskLevelBefore`, `riskLevelAfter` and `riskLevel` (by name). Until #260,
`riskLevel` is the level before.

### Guards

- A trigger refuses a rating on a requirement whose type is not a risk
  type, on a section, and with a value off its project's scales.
- A trigger keeps a type's role while any of its requirements holds a
  rating.
- A trigger keeps a scale's value while any rating, trashed requirements and
  revisions included, uses it, and detection while any rating gives it.
- The scheme is written by the project's administrators alone and read by
  whoever may view the project.

All of it is held by the database and proven straight through SQL as
`exciter_app`.

### Refusals

| Status | Code | When |
|---|---|---|
| 409 | `type_role_held` | turning a type back while its requirements hold ratings |
| 409 | `risk_scale_held` | removing a value a rating uses, or switching detection off while a rating gives it |
| 409 | `ratings_do_not_fit` | moving a rated risk to a project whose scales lack its values |
| 422 | `validation_failed` | a scheme incomplete or too large; a rating on what is not a risk, without a scheme, or off the scales |

### MCP

`get_risk_scheme` reads the scheme; `get_requirement` and `get_spec` carry
ratings with their levels; `create_requirement` and `update_requirement`
take them.

### UI

- The type list gives a type a role: requirements, test cases or risks.
- Project settings gain **Risks**: the scales, the levels with their colours,
  and the matrix or the thresholds, starting from a five by five matrix.
- Writing a risk, the editor gains the two ratings, a choice of each value
  by name, and the level each gives as it is chosen.
- The requirement page shows them; history shows what changed.

### As built (#259)

- **Migrations**: `01260_risk_scheme.sql` adds `risk_scheme`,
  `risk_scale_level`, `risk_level` and `risk_matrix_cell`, kept by
  `risk_scheme_keeper`, and `risk_rpn`, `risk_level_of` and
  `risk_level_ref`, plain SQL so a query over many risks inlines them.
  `01261_risk_ratings.sql` adds the role `risk` and `risk` to `requirement`
  and `requirement_revision`, guarded by `requirement_risk_fields`,
  `risk_scale_level_held` and `risk_scheme_detection_held`;
  `requirement_type_role_held` now holds either role through
  `requirement_type_role_holds`; `risk_view` answers a rating's priority
  numbers and levels.
- **The API** answers a risk's rating as one object, `risk`: `before` and
  `after` with their values, `beforeRpn`, `afterRpn`, `beforeLevel`,
  `afterLevel`, and `state` and `level`, which a revision leaves null. It is
  written as `before` and `after`, replacing the rating whole; empty clears
  it. A scheme's scales answer `held`, the values a rating uses.
- **Saving a scheme** keeps each value by its number, renamed as sent, and
  lays the levels and the matrix anew, the matrix naming each cell's level.
- **Copies, reuse and restores** keep the values the target project's scales
  hold and drop the rest; a move refuses instead.
- **CSV** names the columns `severity`, `probability`, `detection`,
  `severityAfter`, `probabilityAfter` and `detectionAfter`; **ReqIF** names
  its field `Risk`.
- **Web**: the type form picks a role from requirements, test cases and
  risks; Settings, Risks starts from a five by five matrix.

## #260 Mitigations, the FMEA table and the risk matrix

| Operation | Needs | Answers |
|---|---|---|
| `POST /requirements/{reqKey}/links` | edit | a `mitigates` link to a risk; 422 to anything else |
| `GET /requirements/{reqKey}/mitigations` | view | `{mitigations, state}` of a risk |
| `GET /projects/{projectKey}/risks?q=&side=&severity=&probability=&limit=&offset=` | view | `{risks, total, unrated}`: the FMEA rows |
| `GET /projects/{projectKey}/risks.xlsx?q=` | view | an Excel workbook |
| `GET /projects/{projectKey}/risk-matrix?q=&side=` | view | `{matrix}` |

### Mitigation

- A link type may count as `mitigation`, beside decomposition and
  verification. `mitigates` ("mitigates" / "is mitigated by") is seeded
  for every project: its source is the requirement that mitigates, its
  target the risk. A link that counts as mitigation joins a requirement
  that is not a risk to a risk, or is refused.
- `mitigates` goes suspect when its source changes, as a requirement whose
  mitigation changed must be looked at again; re-rating a risk leaves its
  mitigations as they are.
- `leads_to` ("leads to" / "follows from", counting as nothing) is seeded
  too, for how one kind of risk leads to another.
- A mitigation link counts towards no coverage level.

### When a mitigation is in effect

- A mitigation is **in effect** when its source is live, its link is not
  suspect, and the source is implemented and verified in full by the
  coverage rules of its own project (#38). A level the rules do not ask of
  the source's type, `na`, counts as met.
- A risk is:
  - `unmitigated` while nothing mitigates it;
  - `claimed` while a mitigation is not in effect;
  - `mitigated` once every mitigation is in effect.
- The **effective** side of a risk is after once it is mitigated and the
  side after is complete, and before otherwise. Its level is the risk's
  **level**.

### The FMEA table

- One row a risk of the project, in the order of the tree, filtered by a
  query (#47) and paged by `MaxRows`: its key, title, type and attributes,
  the rating before with its level and priority number, its mitigations
  with each one's state, the rating after, the state and the effective
  level. A risk without a rating is in it, as unrated.
- `side`, `severity` and `probability` narrow it to one cell of the risk
  matrix.
- It is edited in place, as the grid (#20): a rating or an attribute
  changed with the revision it was read at, refused as 409
  `revision_conflict` when the risk has moved on.
- **Excel**: one sheet, a row a mitigation with the risk's columns on its
  first row, each level filled in its colour. Downloading it is audited as
  an export.

### The risk matrix

Severity across, probability down, as many risks in each cell as fall in
it, `before` or `effective` (the default). Each cell has the level the
matrix gives it, and opens its risks. Risks without a complete side are
counted as unrated. With a priority number, the cells carry no level.

### Guards

- A link of a type that counts as mitigation joins what is not a risk to a
  risk, held by a trigger.
- A link type does not start counting as mitigation while links of it join
  anything else.

All of it is held by the database and proven straight through SQL as
`exciter_app`.

### Refusals

| Status | Code | When |
|---|---|---|
| 409 | `revision_conflict` | an edit in the FMEA table against an older revision |
| 409 | `link_type_held` | making a link type count as mitigation while its links join what is not a risk |
| 422 | `validation_failed` | a mitigation of what is not a risk, or by a risk |

### MCP

`get_risk_mitigations`, `list_risks` and `get_risk_matrix` read;
`link_requirements` makes a mitigation.

### UI

- A risk's page lists its mitigations, each with its state, and adds one.
- **Risks** in a project's navigation, `/p/{projectKey}/risks`: the FMEA
  table with its filter, edited in place and exported as Excel; and
  `/p/{projectKey}/risks/matrix`, the matrix before and after, a cell
  opening its risks.
- The link-type settings offer mitigation as what a type counts.

### As built (#260)

- **Migration**: `01280_mitigation.sql` lets `link_type.counts` be
  `mitigation` and seeds `mitigates` and `leads_to`, in a new installation
  and in one running already unless a link type has the key or the name;
  stepping back removes them while no link had them. `trace_link_mitigates_risk`
  and `link_type_counts_held` guard the links; `risk_mitigations` reads a
  risk's mitigations, `risk_mitigation_rows` many risks' at once, and
  `risk_mitigation_state` and `risk_effective` give the state and the side
  in effect, which `risk_view` now answers. A type of risks keeps its role
  while a mitigation points at one of its risks.
- **Every reader sees the same state**: a risk's mitigations are read with
  the database's own rights, and a source the reader may not view answers
  `hidden`, named by nothing.
- **The FMEA table and the matrix** read every mitigation of the risks kept
  in one query and work out the levels and states in Go
  (`risk.Scheme.Level`), so a hundred risks of three mitigations read in
  well under three seconds.
- **Queries** gain `mitigation`; `riskLevel` is now the level in effect.
- **Web**: Risks in a project's navigation, `/p/{projectKey}/risks`, with the
  FMEA table and, at `?view=matrix`, the risk matrix; a cell lists its
  risks below the matrix. The table edits a rating in place; it shows a
  risk's attributes, which are edited on the risk's page. A risk's page
  lists its mitigations.

## #261 Risks in traceability, baselines, the report and templates

| Operation | Needs | Answers |
|---|---|---|
| `GET` graph and chain operations | view | a risk's mark |
| `POST /projects/{projectKey}/baselines` | edit | records the risks it holds |
| `GET /projects/{projectKey}/risk-report?q=` | view | `{report}` |
| `GET /projects/{projectKey}/risk-report.docx?q=&template=` | view | a Word file |
| `GET /projects/{projectKey}/risk-report.pdf?q=` | view | a PDF; 503 `render_unavailable` |
| `GET /project-templates` | signed in | `medical` and `safety` beside the others |

### Marks

A risk in the trace graph and the chain report carries its **mark**: its
level, by name and colour, and its state.

### Baselines

A baseline records each risk it holds, with its state and its levels
before, after and in effect, as names; and each of its mitigations, with
its source, whether it was implemented, verified, suspect and in effect.
Its content hash covers them. A baseline taken before has none and keeps
its hash; a scheme changed later does not change what it recorded.

### The risk report

The project, its scheme, how many risks fall at each level before and in
effect, and each risk with its ratings, levels, state and mitigations, in
the order of the FMEA table, filtered by a query. As Word through a
template of the project (#214), and as PDF from its print view,
`/print/risks/{projectKey}`. Downloading it is audited as an export.

### Templates

- **`medical`**, a medical device risk file: types Hazard, Hazardous
  situation and Harm (risks) and Risk control; severity and probability of
  five values each, a matrix of low, medium and high; links `mitigates`,
  `leads_to`, `verifies` and `relates`; coverage asks a risk control to be
  implemented and verified.
- **`safety`**, a functional safety analysis: types Failure mode (risks)
  with Effect and Cause, and Safety requirement; severity, occurrence and
  detection of ten values each, a priority number with thresholds;
  coverage asks a safety requirement to be implemented and verified.
- A template's type may carry a role and an attribute may show on some
  types only.

### Guards

What a baseline records is frozen, as the rest of a baseline.

### UI

- The trace graph and the chain report mark a risk with its level and state.
- A baseline shows what it recorded of each risk.
- The FMEA table exports the risk report as PDF and Word.
- The project chooser offers the two templates.

### As built (#261)

- **Migration**: `01300_risk_traceability.sql` records a baseline's risks as
  two more kinds of `baseline_evidence`, `risk` and `mitigation`, each with
  a `detail` holding what `risk_view` answered of the risk, or the
  mitigation's link, source, coverage and whether it was in effect; the
  content hash writes them as `risk` or `mitigation` and the detail as jsonb
  writes it, which orders its keys the same way every time. `risk_mark`
  answers a risk's level and state for the marks.
- **A baseline's document** shows each risk's levels as recorded, so a
  scheme changed later changes nothing it shows; a baseline taken before
  shows them as the scheme gives them now.
- **Code**: `trace.GraphNode.Risk` and `ChainNode.Risk` carry
  `requirement.RiskMark`; `risk.Report` with `docx.RiskReportBytesWith`;
  `GET /projects/{projectKey}/risk-report` with `.docx` and `.pdf` (MCP
  `get_risk_report`), refused with 409 `risk_scheme_missing` in a project
  without a scheme.
- **Templates**: `project.Blueprint` gains `Risk` and `Coverage`, and an
  attribute spec names the types it shows on; `risk.SeedProject` makes what
  `requirement.Seed` makes and then the scheme. The medical template's
  default type is Risk control, the safety template's Safety requirement.
- **Web**: the chain report and the trace graph's chosen node mark a risk;
  the Risks page downloads the report as PDF and Word; `/print/risks/{key}`
  prints it.

## What is left for later

- Signing off a risk as acceptable as a workflow of its own; a level marks
  what is acceptable meanwhile.
- Weighing benefit against risk.
- Schemes shared by several projects, and more than one matrix a project.
- Fault trees.
- Importing an FMEA table from Excel.
