# API contract for M9: Verification evidence

M9 brings the evidence that a requirement holds into Exciter:
- CI uploads the test results of every run, and each test case names the
  requirements it verifies
- a scan of a repository uploads the tags in its source comments that name
  the requirements the code implements or verifies
- evidence names the revision it was made against, and a newer revision
  makes it outdated, as a suspect link is
- coverage can count tests and code instead of links, and the matrix, the
  chain report and the query language follow
- a trace check answers pass or fail for a pipeline to stop on

The epic is #223; this file is #224. The route table in
`backend/internal/httpapi/operations.go` and `operations_m9.go` is the source
of truth for paths, fields and types, and this file says what they mean.
Operations it adds carry `pending: true` and answer 501 until their issue is
built. Parameters and fields it adds to operations that exist arrive with
their issue.

All paths are under `/api/v1`. Every operation needs somebody signed in or a
token. "view", "edit" and "administer" are the project rights of #12. A
project or requirement the caller may not view is 404, never 403, and a
change in an archived project is 409 `project_archived`. A refusal is a
sentence that says what to do, as in M1.

## Migrations, packages and settings

- Migration ranges, in build order:
  - 00960 to 00979 for test results (#200)
  - 00980 to 00999 for code tags (#201)
- Packages:
  - `internal/testresult` (#200): the JUnit reader, uploads, results, the
    counting branch and retention.
  - `internal/codetrace` (#201): the scanner, scans, tags and tag words.
  - `internal/trace` gains the trace check and judges coverage from
    evidence; `internal/rql`, `internal/baseline` and `internal/notify`
    follow.
  - `cmd/exciter-ci` is the command line a pipeline runs: `results` (#200),
    `check` (#200) and `scan` (#201).
- Settings: none in the environment. Each project names its counting branch
  and its tag words.
- Images: `ghcr.io/cloudster1/exciter-ci`, released with the others, holds
  `exciter-ci` alone. The chart does not run it; a pipeline does.
- Limits:
  - `testresult.MaxRunMB` = 20 and `testresult.MaxRunFiles` = 100: the
    JUnit files of one upload, together.
  - `testresult.MaxRunCases` = 20000: test cases in one upload.
  - `testresult.MaxKeysPerCase` = 20: requirements one test case names.
  - `testresult.MaxNameLength` = 200: repository, branch and job.
  - `testresult.MaxURLLength` and `codetrace.MaxURLLength` = 2000.
  - `testresult.MaxMessageChars` = 2000: the failure message kept per
    result.
  - `testresult.MaxReported` and `codetrace.MaxReported` = 200: keys and
    tags reported back; the rest are counted.
  - `testresult.KeptRuns` = 20: finished runs kept per repository, branch
    and job.
  - `testresult.IdleBranchDays` and `codetrace.IdleBranchDays` = 90: how
    long a branch that does not count keeps its evidence after its last
    upload.
  - `codetrace.MaxScanMB` = 16 and `codetrace.MaxScanTags` = 50000: one
    scan.
  - `codetrace.MaxPathLength` = 1000.
  - `codetrace.MaxWords` = 10 per relation, `codetrace.MaxWordLength` = 30.
  - `trace.MaxCheckItems` = 500: each list a trace check answers.
- **Decided** (`docs/decisions.md`, 2026-10-17):
  - an `evidence` scope on the tokens there are, not a new kind of token
  - the branch that counts is a project setting, and the trace check names
    its own
  - a result names the revision current when the run was uploaded
  - a tag carries its revision forward from the scan before
  - evidence extends the signed content hash, and a baseline signed before
    keeps its hash

## The evidence scope

| Operation | Needs | Answers |
|---|---|---|
| `POST /tokens` | as today | `scopes` takes `["evidence"]` |
| app grants | the consent page | a grant can be `evidence` |

- **A personal token** takes the scope `evidence`, alone: `scopes` is
  `[]`, `["read"]` or `["evidence"]`. Anything else is 422 on `scopes`,
  "That is not a token scope. Use read or evidence, or leave the scopes
  empty."
- **An app's grant** is full, read or evidence. `oauth_grant` and
  `oauth_request` gain `scope` (`full`, `read` or `evidence`) in place of
  `read_only`. An app asks for it with the OAuth scope `evidence`, beside
  `mcp` and `mcp:read`, and the consent page says what it reaches.
- **What it reaches.** An evidence token, personal or an app's, reaches:
  - `POST /projects/{projectKey}/test-runs` and `GET /test-runs/{runID}`
  - `POST /projects/{projectKey}/code-scans`, `GET /code-scans/{scanID}`
    and `GET /projects/{projectKey}/code-tag-words`
  - `GET /projects/{projectKey}/trace-check`
  - `GET /auth/me`

  The route table marks these `evidence`. Anything else, a read included,
  is 403 `evidence_token`, "This token uploads evidence and runs the trace
  check alone. Use a token without the evidence scope for anything else."
  An app's evidence token is refused at `/mcp` the same way, and an
  evidence token is good outside `/mcp`, where the other app tokens are
  not.
- **It acts as its person.** Uploading needs edit on the project, as the
  token's person holds it, and a token limited to projects reaches those
  alone. What it writes carries `via` (#190).
- **Refreshing.** An app refreshes its token in one place and hands each
  job the access token, which lasts an hour. A refresh token used twice
  ends the grant, as #182 agreed, so `exciter-ci` never refreshes.

## #200 Test results from CI

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/test-runs` | edit | 201 `{run}`; 403; 413 `too_large`; 422 |
| `GET /test-runs/{runID}` | the uploader, or administer | `{run}` |
| `GET /requirements/{reqKey}/test-results` | view | `{results, countingBranch}` |
| `GET /projects/{projectKey}/evidence-settings` | view | `{settings}` |
| `PUT /projects/{projectKey}/evidence-settings` | administer | `{settings}`; 403; 422 on `countingBranch` |
| `GET /projects/{projectKey}/trace-check` | view | `{check}`; 400; 422 `bad_query` |

### Uploads

- **The form** is `multipart/form-data`:
  - `repository`: a name such as `acme/checkout`, at most `MaxNameLength`
  - `commit`: the commit's SHA, 7 to 64 hex digits
  - `branch`: at most `MaxNameLength`
  - `job`: optional, tells apart the runs of one commit, such as unit and
    browser tests; empty is one job
  - `runUrl`: optional, the CI run's address, `http` or `https`
  - `file`: one part or more, each a JUnit XML file

  A field missing or too long is 422 on it; a `runUrl` of another scheme
  is 422 on `runUrl`, "Give the run's address as an http or https link."
  Files over `MaxRunMB` together are 413 `too_large`, over `MaxRunFiles`
  422 on `file`.
- **The upload is read at once and run in the worker**, as an import is
  (#10). A file that is not JUnit XML is 422 on `file`, "<name> is not a
  JUnit XML file. Upload the XML report your test runner writes." The run
  answers 201 in state `queued`; the worker takes it to `running` and
  `done`, or `failed` with `error`. `exciter-ci results` reads
  `GET /test-runs/{runID}` until it is done, from the primary, so it sees
  the worker's writes.
- **JUnit as test runners write it**: `testsuites` or a single `testsuite`,
  nested suites, `testcase` with `classname` and `name`, and `failure`,
  `error` and `skipped` inside it. A case with none is `passed`. The suite
  is the case's `classname`, or its suite's name without one.
- **A test case names the requirements it verifies** by key, in any of:
  - its name, as `[verifies CHK-R42]`, several keys separated by commas or
    spaces
  - a `property` named `verifies` or `requirement` whose value is keys
  - a tag, as runners that write `properties` for tags do, named
    `verifies`

  Until #201 the word is `verifies`; from #201 on, the project's verify
  words. A key resolves through `requirement_key_id`, so a key from before
  a project was renamed still reaches its requirement, and only within the
  run's project.
- **What a run reports back** is each key that verifies nothing, with the
  test that named it: `unknown` (no such requirement), `trashed` or
  `section`. At most `MaxReported`, then counted in `unreported`.
  `exciter-ci results` prints them.
- **A run keeps** repository, commit, branch, job, run address, counts by
  outcome, cases read and cases matched, who uploaded it and `via`.

### Results and outdated

- **A result** is one test case's outcome for one requirement it named:
  `passed`, `failed`, `skipped` or `errored`, the failure message cut to
  `MaxMessageChars`, the duration, and the requirement's revision.
- **The revision it names** is the one current when the run was uploaded:
  the newest of the requirement's revisions made at or before
  `uploadedAt`. The database stamps it, so a revision saved while the
  worker read the run never counts as tested.
- **Outdated.** A result is outdated once the requirement has a revision
  past it, as a suspect link is (#36): it says it was made against an
  older text. It stays shown, marked.
- **The latest result** of a test is its result in the latest finished run
  of its repository, branch and job; a test the latest run no longer holds
  is no longer evidence.
- **`GET /requirements/{reqKey}/test-results`** answers the latest result
  per test, repository, branch and job, counting branch first, each with
  `outdated`, `counts` (it is on the counting branch), its commit, run and
  address. `branch` keeps one branch.
- **Armature.** Where a result's commit is in the development of an
  Armature issue the requirement links to, read with the caller's own
  Armature token, the result carries `armature: {issueKey, url}`, the
  commit's address there. A SHA matches by prefix, and an Armature without
  the development panel answers nothing.

### The counting branch

- **A project names one branch** whose evidence counts for coverage, the
  matrix, the chain report and the query language: `main` until an
  administrator names another, at most `MaxNameLength`. Saving it is
  audited as `evidence_settings.saved`.
- Results on other branches are shown on the requirement and checked by a
  trace check that names their branch, and count nowhere else.

### Retention

- The worker keeps `KeptRuns` finished runs per repository, branch and job,
  never the latest one, and drops a branch's runs `IdleBranchDays` after
  its last upload unless it is the counting branch.
- A baseline keeps what it recorded of them (below), so pruning never
  changes a baseline.

### Coverage from evidence

- **`coverage_rule` gains `verifiedFrom`**, the sources that count for
  verified: `links` and `tests`, at least one; `["links"]`, today's
  meaning, when absent. #201 adds `code`, and `implementedFrom` with
  `links` and `code`.
  - `links` for verified is today's: trace links of a type counting
    verification, and Armature issues that verify. For implemented it is
    Armature issues that implement.
  - `tests` are the latest results on the counting branch.
  - `code` is the counting branch's code tags.
- **The level** over the sources a rule lists:
  - `none` when none of them holds anything
  - `full` when every source that holds something holds it whole: links as
    today, tests all `passed` and current, code all current
  - `partial` otherwise

  A source that holds nothing does not stop another from making a
  requirement `full`. `skipped` is never whole.
- **Judged once in SQL.** `requirement_evidence_counts(id)` counts a
  requirement's tests and tags by outcome and state on the branch
  `app.evidence_branch` names, or the counting branch. The coverage report
  (`judgeLinks`) and the query language's `requirement_coverage` both read
  it, and `trace.Judge` judges from the counts, so the report, the grid and
  the workflow condition `coverage_full` agree; an integration test holds
  them to the same answers.
- Coverage is judged again when a run or a scan is done, the counting
  branch changes or the tag words do.

### The query language

- `testResult`, an enum: the worst latest result of the requirement's tests
  on the counting branch, `errored`, `failed`, `skipped`, `passed`, or
  `none` without tests.
- `testOutdated`, a number: its tests whose latest result is outdated.
- #201 adds `codeLink`, its code tags, and `codeOutdated`, those outdated,
  both numbers.

Examples: `testResult = failed`, `verified != full AND testOutdated > 0`.

### The matrix and the chains

- **The matrix** takes `columns=tests`: the test cases of the counting
  branch as columns, narrowed by `columnRepository`, each cell the latest
  result, outdated marked. #201 adds `columns=code`, the files tagged, each
  cell the tags. The CSV and the workbook follow; a baseline's matrix lays
  these from what the baseline recorded.
- **The trace model** takes levels of kind `tests`, reached by `verifies`,
  and, from #201, `code`, reached by `implements` or `verifies`. As for
  issues and pages, only the last level may be one. A test node carries
  `result` and `outdated`; a chain whose last node is a failing or outdated
  test is broken there.

### The trace check

- **`GET /projects/{projectKey}/trace-check`** narrows like the grid
  (`q`, `query`, `type`, ...) and judges, on `branch` or the counting
  branch:
  - `broken`: chains of the trace model that stop short, by root, the keys
    reached and the level missing
  - `gaps`: requirements whose coverage asks for a dimension not `full`
  - `failing`: latest results `failed` or `errored`
  - `outdated`: outdated results, and from #201 outdated tags
  - `orphaned`, from #201: tags naming a requirement in the trash or none

  `fail`, repeated, names what fails the check: `broken`, `gaps` and
  `failing` when absent. An unknown value is 400.
- **It answers 200** whether it passes or not: `{verdict, branch, fail,
  chains, broken, gaps, failing, outdated, orphaned, totals, evidence,
  checked}`. Each list holds at most `MaxCheckItems`; `totals` counts them
  whole. `chains` is false when the project has no trace model, so nothing
  was followed. `evidence` counts the runs and scans of the branch, so a
  pipeline sees a check over nothing.
- **`exciter-ci check`** prints what failed and exits 1 on `fail`, 0 on
  `pass`, and 2 when it cannot ask.

### Baselines

- **A baseline records the evidence it held**: per item, each latest
  result of the counting branch and, from #201, each tag, as frozen copies
  with the commit and addresses inline, so pruning a run changes nothing.
  `baseline_evidence` is written in the taking transaction, by
  `baseline.Service.Take` and by a session's closing baseline alike.
- **The signed content hash** (M8) gains, after its other lines, one line
  per piece of evidence ordered by requirement, kind and name:
  `evidence <requirement id> test <repository> <job> <suite> <name> <revision> <outcome>`
  and, from #201, `evidence <requirement id> code <repository> <path> <line> <word> <revision>`.
  A baseline without evidence has no such line, so every baseline signed
  before M9 keeps its hash.

### Refusals

- 403 `evidence_token`, above.
- 404 for a run that is not the caller's to read; a run is its uploader's
  and the project's administrators'.
- 422 on a form field, 413 `too_large`, 400 for a `fail` or `branch` that
  does not parse, 422 `bad_query` for a query, as the grid answers it.

### MCP

`get_test_results` reads `GET /requirements/{reqKey}/test-results` and
`run_trace_check` the trace check. No tool uploads.

### UI

- The requirement page gains a test results panel: per test, its outcome,
  when, which commit and run, linking to both, outdated marked; the
  counting branch first, others below.
- The coverage page's rules gain "verified from"; the project's settings
  gain the counting branch.
- The token page and the consent page offer the evidence scope.

### As built (#200)

- **Migrations**:
  - `00960_evidence_scope.sql`: the token CHECK, `evidence` on
    `oauth_request` and `oauth_grant`.
  - `00961_test_results.sql`: `evidence_settings`, `test_run`,
    `test_result`, `test_run_latest`, `coverage_rule.verified_from`, and
    the functions `evidence_counting_branch`, `evidence_branch`,
    `requirement_test_counts` and `requirement_test_result`, with
    `requirement_coverage` re-created.
  - `00962_baseline_evidence.sql`: `baseline_evidence`, with the hash
    functions re-created.
  - `00963_trace_tests_level.sql`: the `tests` level kind.
- **An app's grant** keeps `read_only` and gains a boolean `evidence` beside
  it, never both, rather than one `scope` column: the access token reads
  the grant's, so no token row changes. `scopeAsked` in `internal/oauth`
  reads `evidence` alone.
- **The route table** marks evidence rows `evidence: true`, and
  `evidenceToken` beside `wholeOrg` refuses every other route, MCP
  included. `principalOf` authenticates an access token before it refuses
  one outside MCP, so an evidence one passes.
- **A multipart row** names its form fields with `request` and says a file
  part may come more than once with `files`; `spec.go`'s
  `multipartSchema` lays them out beside `file`, and `uploadedRun` reads
  them.
- **The guards**:
  - `test_run_guard` keeps a run as uploaded and its state moving forward.
  - `test_run_finished`, SECURITY DEFINER, makes a done run the latest of
    its repository, branch and job.
  - `test_result_stamped` stamps the revision from the run's `uploaded_at`.
  - The app role holds no UPDATE or DELETE on results and no write on
    `test_run_latest`.
- **Results are keyed** by run, requirement, suite and name; there is no
  table of test cases.
- **The worker** routes `test_run.requested` to `testresult.Handler`, which
  restores the uploader's token projects. `testresult.Retention` prunes
  hourly as the admin role. `test_run.finished` and
  `evidence_settings.saved` clear the coverage cache.
- **Armature**: `armature.Service.Commits` reads
  `GET /issues/{key}/development` of the requirement's linked issues as the
  reader. The stub serves it, with `POST /_stub/{tenant}/issues/{key}/commits`
  to tell it of a commit.
- **The trace check** narrows with the grid's parameters (`q`, `query`,
  `type`, ...). A chain ending in a test that is not passing and current is
  broken there, its `missing` "passing test".
- **A baseline's matrix** lays out requirements, issues and pages as before,
  and refuses `columns=tests` with 422: the evidence it recorded shows on
  its requirements and is in its signed content.
- **`exciter-ci`** lives in `backend/cmd/exciter-ci` and is built by
  `deploy/Dockerfile.ci`. Compose runs it as `ci` under the `tools` profile,
  for `make ci-image`, `make test-results` and `make trace-check`.
  `tests/test-release-workflow.sh` knows it as a tool image the chart does
  not run.
- **The web**: `features/evidence` holds the results panel on the
  requirement page and the counting branch card on the trace settings tab.
  The coverage rules choose what verifies, the matrix and the trace model
  take tests, and the token, consent and connected-apps pages offer and
  name the evidence scope.

## #201 Code tags

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/code-scans` | edit | 201 `{scan}`; 403; 413 `too_large`; 422 |
| `GET /code-scans/{scanID}` | the uploader, or administer | `{scan}` |
| `GET /requirements/{reqKey}/code-links` | view | `{links, countingBranch}` |
| `GET /projects/{projectKey}/code-tags` | view | `{tags, total, limit, offset}`; 422 on `state` |
| `GET /projects/{projectKey}/code-tag-words` | view | `{words}` |
| `PUT /projects/{projectKey}/code-tag-words` | administer | `{words}`; 403; 422 |

### Tags

- **A tag** sits in a source comment: `[<word> <key>]` or
  `[<word> <key>@<revision>]`, such as `[implements CHK-R42]` or
  `[verifies CHK-R42@3]`. Several keys may follow one word, separated by
  commas.
- **The words** are the project's: `implements` and `verifies` by default,
  at most `MaxWords` per relation, each a letter followed by letters,
  digits, `-` or `_`, at most `MaxWordLength`, and no word in both.
  Saving them is audited as `code_tag_words.saved`. A tag keeps the word it
  was written with and is read by the words of the day, so changing them
  needs no new scan.
- **`GET /projects/{projectKey}/code-tag-words`** answers the words and
  `projectKeys`, the project's key and its former ones, which is what the
  scanner looks for.

### Scans

- **The scanner** (`exciter-ci scan`) reads a checked-out repository in CI;
  Exciter clones nothing. It reads comments in `//`, `#`, `/* */`, `--`,
  `<!-- -->`, `;` and `%` styles and in docstrings, skips what `.gitignore`
  and `--exclude` leave out, and uploads what it found.
- **The body** is JSON: `{repository, commit, branch, fileUrl, tags}`.
  - `fileUrl` is the address of a line at a commit, `http` or `https`, with
    `{commit}`, `{path}` and `{line}` in it. The scanner works it out from
    the remote for GitHub, GitLab and Gitea and takes `--file-url`
    otherwise; without one, links have no address.
  - each tag is `{path, line, word, key, revision?}`.

  Over `MaxScanTags`, or a body over `MaxScanMB`, is 413 `too_large`.
- **A branch's scan replaces its scan before**, as one write through
  `code_scan_replace`.
- **A tag's revision** is the one it names. A tag that names none carries
  the revision of the same tag in the scan before (same repository, branch,
  path, word and key); a tag new to the branch is stamped with the
  revision current at upload. So a new scan never makes old code current
  again; only editing the tag does. A revision the requirement does not
  have yet is reported back as `revision`.
- **What a scan reports back**: tags with an `unknown` key, a `trashed` or
  `section` one, a `word` that is not the project's, or a `revision` it
  does not have. At most `MaxReported`, then counted.

### Links and states

- **`GET /requirements/{reqKey}/code-links`** answers each place that
  implements or verifies the requirement: repository, branch, commit, path,
  line, word, relation, revision, `outdated`, `counts` and its address at
  that commit.
- **A tag is** `current`, `outdated` when the requirement was revised past
  its revision, or `orphaned` when its requirement is in the trash or there
  is none. `GET /projects/{projectKey}/code-tags` lists the outdated and
  orphaned ones of a branch, the counting branch by default, a window at a
  time.
- **Coverage, the matrix, the chains and the query language** count code as
  #200 sets out above.
- **The trace check** fails on `orphaned` and on outdated tags when `fail`
  names them.

### Notifications

- `code_outdated`: to the requirement's owner and watchers, when a scan of
  the counting branch brings a tag naming a revision older than the
  current one that the scan before did not have.
- `code_orphaned`: to the requirement's owner and watchers for a tag naming
  a requirement in the trash, and to the project's administrators for a
  key that names none, when the scan before did not have it.

  Each is told once per scan, however many tags it brings.

### MCP

`get_code_links` reads `GET /requirements/{reqKey}/code-links`. No tool
uploads.

### UI

- The requirement page gains a code panel: per place, the file and line
  linking to it, the relation and the revision, outdated marked.
- A project page lists outdated and orphaned tags.
- The project's settings gain the tag words; the coverage rules gain
  "implemented from" and "code" for verified.

## What is left for later

- The trace graph that shows tests and code as nodes is #211's.
- Results of a run streamed while it runs: a run is uploaded once it ends.
- Test management, its cases, plans and runs, is #202; evidence from CI
  stays apart from it until then.
- Notifications of a failing test; the owner sees it on the requirement and
  the trace check fails on it.
- A baseline's matrix of the tests it recorded: the evidence is in the
  baseline and its signed content, and its matrix refuses `columns=tests`.
