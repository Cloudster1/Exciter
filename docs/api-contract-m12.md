# API contract for M12: Test management

M12 brings test management beside the requirements:
- a requirement type marked as test cases gives its requirements
  preconditions, steps with an action and an expected result, and parameters
- test cases are grouped into test plans, and a test execution runs a plan
  against a baseline, a version and a configuration, with testers recording
  each step's result, files and the issues a failure raises
- results from CI fill the executions they belong to, and executed test
  cases count as verification in coverage, the matrix, the chains and
  queries

The epic is #202; this file is #253. The route table in
`backend/internal/httpapi/operations.go` and `operations_m12.go` is the
source of truth for paths, fields and types, and this file says what they
mean. Operations it adds carry `pending: true` and answer 501 until their
issue is built. Parameters and fields it adds to operations that exist
arrive with their issue.

All paths are under `/api/v1`. Every operation needs somebody signed in or a
token. "view", "comment", "edit" and "administer" are the project rights of
#12. A project, requirement, plan, execution or file the caller may not view
is 404, never 403, and a change in an archived project is 409
`project_archived`. A refusal is a sentence that says what to do, as in M1.

## Migrations, packages and settings

- Migration ranges, in build order:
  - 01200 to 01219 for test cases (#250)
  - 01220 to 01239 for test plans and executions (#251)
  - 01240 to 01259 for executions in traceability (#252)
- Packages:
  - `internal/requirement` gains a test case's preconditions, steps and
    parameters (#250), checked in `testcase.go`.
  - `internal/testexec` (#251): test plans and executions, recording,
    files, issues filed from failures and the report. It sits above
    `internal/requirement`, `internal/search` and `internal/armature`,
    which it reads; nothing below reads it.
  - `internal/testresult` hands what CI reports to open executions (#252);
    `internal/trace` counts executions as a coverage source.
- Settings: none in the environment. A project's coverage rules may name
  test executions as a source of verified (#252).
- Limits:
  - `requirement.MaxSteps` = 100 steps a test case;
    `requirement.MaxParameters` = 10 parameters of at most
    `requirement.MaxParameterValues` = 50 values of at most
    `requirement.MaxParameterValueLength` = 200 characters.
  - `testexec.MaxPlanCases` = 500 cases a plan; `testexec.MaxExecutionCases`
    = 2000 case iterations an execution; `testexec.MaxTesters` = 50.
  - `testexec.MaxTitleLength` = 100, `testexec.MaxDescriptionLength` = 500,
    `testexec.MaxVersionLength` = 200, `testexec.MaxConfigurationLength` =
    500, `testexec.MaxNoteLength` = 4000 characters.
  - `testexec.MaxFileMB` = 50, as an attachment (#21); at most
    `testexec.MaxFilesPerStep` = 20.
- **Decided** (`docs/decisions.md`, 2026-10-18):
  - a test case is a requirement of a test-case type, not a kind or a table
    of its own
  - a run is called a test execution, since a test run is an upload from CI
  - results from CI fill open executions
  - an issue filed from a failure links with `relates`
  - a tester records when assigned and able to comment

## #250 Test cases

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/types`, `PATCH .../types/{typeID}` | administer | `role` |
| `POST /projects/{projectKey}/requirements`, `PATCH /requirements/{reqKey}` | edit | `preconditions`, `steps`, `parameters` |
| `GET /requirements/{reqKey}`, its revisions and history | view | `preconditions`, `steps`, `parameters`; a step diff |

### The role of a type

- A requirement type has a `role`: `requirement`, as every type has today,
  or `testCase`. Only a type's role makes its requirements test cases; they
  stay requirements in every other way, with a key `KEY-R<n>`, revisions,
  status, attributes, labels, links and a place in the tree.
- A type keeps `testCase` while any of its requirements, trashed ones
  included, holds preconditions, steps or parameters; turning it back is
  409 `type_role_held`, which names how many hold them.

### Preconditions, steps and parameters

- **Preconditions**: a rich text document, null for none.
- **Steps**: in order, at most `MaxSteps`; each has an `action`, a rich text
  document, and an `expected` result, a document or null. A step with an
  empty action is refused.
- **Parameters**: at most `MaxParameters`, each a `name` and its `values`.
  Every parameter has as many values as the others; iteration *n* takes the
  *n*-th value of each. No parameters is one iteration. Names are unique
  in a test case, ignoring case.
- Only a requirement, not a section, of a test-case type holds them; on any
  other they are refused as 422 on the field.
- **Changing the type** of a test case to one that is not a test-case type
  drops them, which is a change of normative content and writes a revision.

### Revisions and hashes

- They are part of every revision, of the normative content and of its
  hash, so a change to a step writes a revision, a link verifying the case's
  target goes suspect as any revision makes it (#36), a baseline holds them,
  and a lock (#205) keeps them.
- The hash leaves them out when they are empty, so every requirement that
  holds none keeps the hash it has, and every baseline its signature.
- History compares steps one by one: added, removed, changed or moved.

### What shows them

The requirement page, the print view and the document (#45), its Word and
Markdown exports, and `get_requirement` show preconditions, the parameters
as a table of iterations, and the steps as a table of action and expected
result.

### CSV and ReqIF

- **CSV** (#41): columns `Preconditions` (Markdown, as the statement),
  `Steps` (a Markdown table with the columns Action and Expected) and
  `Parameters` (one `name: value; value` per line). An import reads them
  back for test cases and refuses them on anything else.
- **ReqIF** (#44): `exciter.preconditions` and `exciter.steps` (XHTML, the
  steps as a table) and `exciter.parameters` (a string, as in CSV). They are
  read back from Exciter's own definitions; another tool's attributes of the
  same name are ordinary attributes.

### Guards

- A trigger refuses preconditions, steps or parameters on a requirement
  whose type is not a test-case type, and on a section.
- A trigger keeps a type's role while any of its requirements holds them.
- A type's role is changed by the project's administrators alone, as the
  rest of a type.

All of it is held by the database and proven straight through SQL as
`exciter_app`.

### Refusals

| Status | Code | When |
|---|---|---|
| 409 | `type_role_held` | turning a type back while its requirements hold steps |
| 422 | `validation_failed` | steps or parameters on what is not a test case, too many, a step without an action, parameters of uneven length or with a name twice |

### MCP

`get_requirement` and `get_spec` carry them; `create_requirement` and
`update_requirement` take them, with action and expected as Markdown.

### UI

- The type list marks a type as test cases with a switch, disabled with the
  reason while it is held.
- Writing a test case, the editor gains preconditions, steps in order with
  buttons to add, move and remove, and a table of parameters.
- The requirement page shows them read-only; history shows the step diff.

### As built (#250)

- **Migration**: `01200_test_cases.sql` adds `requirement_type.role`, and
  `preconditions`, `steps` and `parameters` to `requirement` and
  `requirement_revision`, guarded by `requirement_test_case_fields` and
  `requirement_type_role_held`; `attachment_referenced` counts the files a
  test case's preconditions and steps show.
- **The API** answers what a test case carries as one object, `testCase`
  (`preconditions`, `steps`, `parameters`), on a requirement, a revision, a
  specification entry and a document entry, null for any other; it is
  written the same way, a field left out of a change staying as it is. A
  type answers `role` and `roleHeld`.
- **Code**: `requirement/testcase.go` checks, hashes and keeps it,
  `testcase_diff.go` compares steps, `testcase_table.go` writes steps and
  parameters as tables for Word, Markdown and ReqIF. A move, copy, reuse or
  restore onto a type that is not one of test cases drops them.
- **CSV** writes plain text, as it writes the statement: preconditions as
  text, steps numbered with an `Expected:` line each, parameters as
  `name: value; value`. A project with a type of test cases exports those
  columns by default.
- **ReqIF** writes the three fields only on types of test cases; an import
  makes a type of test cases for such a type, and leaves them out as a loss
  where it lands on another type.
- **Left for later**: searching the words of steps and preconditions.

## #251 Test plans and test executions

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/test-plans` | view | `{plans}` |
| `POST /projects/{projectKey}/test-plans` | edit | `{plan}`; 422 |
| `GET /test-plans/{planID}` | view | `{plan}`, its cases resolved |
| `PATCH /test-plans/{planID}` | edit | `{plan}`; 422 |
| `PUT /test-plans/{planID}/cases` | edit | `{plan}`; 422 |
| `DELETE /test-plans/{planID}` | edit | 204 |
| `GET /projects/{projectKey}/test-executions?state=` | view | `{executions}` |
| `POST /projects/{projectKey}/test-executions` | edit | `{execution}`; 422 |
| `GET /test-executions/mine` | signed in | `{executions}` the caller tests or manages, open first |
| `GET /test-executions/{executionID}` | view | `{execution}` with its cases and progress |
| `PATCH /test-executions/{executionID}` | manager | `{execution}`; 403; 409 |
| `POST /test-executions/{executionID}/close` | manager | `{execution}`; 403; 409 |
| `GET /test-executions/{executionID}/cases/{reqKey}?iteration=` | view | `{case}`: its steps at the pinned revision, results, files, issues |
| `PUT /test-executions/{executionID}/cases/{reqKey}/result` | tester | `{case}`; 403; 409; 422 |
| `POST /test-executions/{executionID}/cases/{reqKey}/files?iteration=&step=` | tester | `{file}`; 413 |
| `GET /test-execution-files/{fileID}` | view | the file |
| `DELETE /test-execution-files/{fileID}` | its uploader | 204; 409 |
| `POST /test-executions/{executionID}/cases/{reqKey}/issues` | tester | `{issue}`; 409 `armature_*` |
| `GET /test-executions/{executionID}/report` | view | `{report}` |
| `GET /test-executions/{executionID}/report.docx?template=` | view | a Word file |
| `GET /test-executions/{executionID}/report.pdf` | view | a PDF; 503 `render_unavailable` |
| `GET /requirements/{reqKey}/test-executions` | view | `{results}`: a test case's results, newest first |

### Test plans

- A plan belongs to a project and holds test cases, either **picked**, in
  the order given, or found by a **query** (#47) when an execution starts.
  At most `MaxPlanCases`.
- A picked case is a live requirement of a test-case type of the plan's
  project. A case later trashed stays in the plan and is left out of the
  executions it starts, with the reason.

### Test executions

- An execution runs a plan's cases, once each, or once per iteration of a
  case with parameters. It has a title, an optional **baseline** of the
  project, a **version** and a **configuration**, free text, a **manager**
  (who starts it), a **due date** and **testers**: people and groups, as a
  review session's participants (#204).
- **Starting pins what is run.** With a baseline, each case runs at the
  revision the baseline holds, and a case the baseline lacks is left out;
  without one, each case runs at its current revision. Cases left out are
  listed in `excluded` with the reason. A plan later changed does not
  change an execution.
- It is `open` until its manager closes it, `closed`, or aborts it,
  `aborted`. A closed or aborted execution never changes; an aborted one
  counts for nothing (#252).

### Recording

- A tester records a case iteration: each step's outcome, `passed`,
  `failed`, `blocked` or `skipped`, with the actual result as a note, and
  the case's own outcome and note. The case's outcome, when not given,
  follows from its steps: failed if any failed, else blocked if any is
  blocked, passed if all passed, skipped if all were skipped, and not run
  while a step is open.
- Recording again replaces what was recorded, by whoever records it; the
  audit log keeps each.
- **Files** go on a step or the case, at most `MaxFileMB` each and
  `MaxFilesPerStep` a step, while the execution is open. Their uploader
  deletes them while it is open.

### Who records

The execution's manager, and its testers, in person or through a group,
who may comment in the project. Nobody records in a closed execution.

### Issues from failures

A tester files a failed case as an issue in Armature (#26), with their own
token, as issues are made from requirements (#28): the summary says which case failed in
which execution, the description gives the step, the expected and the
actual result, the version and the configuration, and links back. The issue
is linked to the case and to every requirement the case verifies, with the
relation `relates`, never `verifies`.

### The report

The execution, its baseline, version and configuration, manager and
testers, counts by outcome, and each case with its revision, steps,
outcomes, notes, files by name, issues and whether CI recorded it. As Word
through a template of the project (#214), and as PDF from its print view.
Downloading it is audited as an export.

### Notifications

Testers are told when an execution starts, reminded a day before it is
due, and told when it closes, as a review session's participants are.

### Guards

- A plan's cases are test cases of its project.
- An execution's cases are made only by the function that starts it,
  which pins them; nobody inserts them by hand.
- Results and files are written while the execution is open, by its
  manager or by a tester who may comment, as themselves.
- A closed or aborted execution never changes, and its state moves once.
- A file deleted leaves its object for the reaper, as an attachment's.

All of it is held by the database and proven straight through SQL as
`exciter_app`.

### Refusals

| Status | Code | When |
|---|---|---|
| 403 | `forbidden` | recording without being a tester who may comment, or managing without being the manager |
| 409 | `execution_closed` | anything but reading a closed or aborted execution |
| 409 | `armature_*` | as when issues are made from requirements (#28) |
| 413 | `too_large` | a file over `MaxFileMB` |
| 422 | `validation_failed` | a plan without cases or with what is not a test case, a query that does not compile, a baseline of another project, too many cases, an outcome or step that does not exist |

### MCP

`list_test_plans`, `get_test_plan`, `list_test_executions`,
`list_my_test_executions`, `get_test_execution`, `get_test_execution_case`
and `get_test_case_results` read; `start_test_execution`,
`record_test_result`, `close_test_execution` and `file_test_failure_issue`
write, as their operations allow the caller.

### UI

- **Tests** in a project's navigation, `/p/{projectKey}/tests`, with plans
  and executions; a plan's page picks and orders its cases or takes a
  query with a preview.
- Starting an execution chooses the plan, the baseline, version,
  configuration, due date and testers.
- An execution's page shows progress, every case and its outcome, the
  testers, closing and aborting, and the report.
- **The step runner**, `/p/{projectKey}/tests/executions/{id}/run/{reqKey}`,
  shows one step at a time with its action and expected result, records it
  with a button each for passed, failed, blocked and skipped, the actual
  result and files, steps through iterations and cases, and files an issue.
- Home lists the executions the person tests; a test case's page lists its
  results.

### As built (#251)

- **Migration**: `01220_test_executions.sql` adds `test_plan`, `test_plan_case`,
  `test_execution`, `test_execution_tester`, `test_execution_case` (one row
  per iteration, with the parameters' values it runs), `test_execution_step`,
  `test_execution_file` and `test_execution_issue`. `test_execution_start`
  pins the cases; `test_execution_manages`, `_tests` and `_records` say who
  may do what; a trigger keeps whatever an execution holds from changing
  once it is closed, but for letting go of a person forgotten or a CI run
  reaped. Notifications gain `execution_id` and three kinds.
- **Code**: `internal/testexec` (plans, executions, recording, files,
  issues, the report, the reminder); `armature.FileDraft` files an issue
  with its own words; `docx.ExecutionReportBytesWith`; the worker reminds a
  day before an execution is due.
- **Refusals**: a plan's query that does not compile is 400 `bad_query`, as
  the grid answers it. A tester's link to a requirement they may not link
  is left out; the issue stands.
- **Web**: `features/tests` on `/p/{projectKey}/tests`, a plan's page, an
  execution's page and the step runner; `/print/executions/{id}` for the
  PDF; the home lists the reader's open executions and a test case's page
  its results.

## #252 Executions in traceability

| Operation | Needs | Answers |
|---|---|---|
| `PUT /projects/{projectKey}/coverage-rules` | administer | `verifiedFrom` may name `runs` |
| `GET` coverage, matrix and chain operations | view | test cases' last results |

### Results from CI

- When a CI upload (#200) lands on the project's counting branch, each of
  its results that verifies a test case's key fills that case in every
  open execution holding it: every iteration takes the worst of the
  upload's results for the case, `errored` counting as failed.
- A case a tester recorded is never overwritten, and an upload never
  overwrites a result from a newer one. It happens in the upload's own
  transaction, once.

### Coverage from executions

- A coverage rule's verified may come from `runs` beside links, tests and
  code (#148, #200, #201).
- A test case's **last result** is its outcome in the execution, not
  aborted, that recorded it last: the worst over its iterations, an
  iteration not run counting as not passed.
- A requirement is **verified from runs** when test cases verify it through
  links that count as verification, and the last result of each passed. It
  is **whole** when each of those results is at the case's current revision
  and its link is not suspect, and partial otherwise.

### Queries, chains and the matrix

- The query language gains `testCase` (whether a requirement is one),
  `runResult` (a test case's last result: `passed`, `failed`, `blocked`,
  `skipped` or `none`), `runOutdated` (its last result is at an older
  revision) and `casesResult` (the worst last result of the test cases
  verifying a requirement).
- A test case in the chain report and the matrix carries its last result,
  its execution and whether it is outdated.

### Baselines

A baseline records the last result of each test case it holds, and its
execution, beside the evidence of #200; its content hash covers them. A
baseline taken before has none and keeps its hash.

### Guards

The filling happens in a database function the upload's own transaction
calls, which takes only a landing upload, of the caller, on the counting
branch. It is proven straight through SQL as `exciter_app`.

### UI

- Coverage rules offer test executions as a source of verified.
- The chain report and the matrix mark a test case with its last result.

### As built (#252)

- **Migration**: `01240_test_traceability.sql` adds `test_case_last_run`, a
  case's last result with its execution and pinned revision;
  `requirement_verifying_cases`, `requirement_case_counts` and
  `requirement_cases_result` over the live test cases verifying a
  requirement; and `test_execution_take_ci`, which the upload's landing
  calls after it keeps the results. `coverage_rule.verified_from` may name
  `runs`, `requirement_coverage` counts it, and `baseline_evidence` gains
  kind `run` with `execution_id`, written as `run <execution> <revision>
  <outcome>` in the content hash. The count functions declare that they
  answer one row, so a coverage query's estimate stays below the cost at
  which Postgres compiles it, which took longer than running it.
- **Last results**: an iteration not run makes a case's last result `none`
  unless another iteration failed or was blocked, as a case's outcome
  follows from its steps.
- **Code**: `trace.LastRun` marks a test case among the chain report's
  nodes and the matrix's rows and requirement columns; `Counted` gains
  `Cases` and `CasesWhole`. An upload that fills an execution audits it and
  emits `test_execution.recorded` once per execution; that topic and
  `test_execution.closed` clear the coverage cache.
- **Web**: coverage rules offer "Test executions"; the chain report and the
  matrix's rows show a case's last result, its columns in their title.

## What is left for later

- An "automated" mark on a test case or step; labels serve meanwhile.
- Parameters combined every way rather than lined up.
- Testers per case, and reopening a closed execution.
- Templates of executions and recurring cycles.
- Versions and configurations as lists a project keeps.
- Results from formats other than JUnit.
