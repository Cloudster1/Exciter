# API contract for M5: Traceability

M5 grows the traceability matrix of #38 into something to work in: its axes
and link types chosen, its gaps shown, links made in its cells, Stator pages
as columns, a project's trace model and the chains that follow it, matrix
setups saved as views, the matrix of a baseline, and Excel files. The epic is
#145; this file is #146. The route table in
`backend/internal/httpapi/operations.go` is the source of truth for paths,
fields and types, and this file says what they mean. Operations it adds carry
`pending: true` and answer 501 until their issue is built; parameters and
fields it adds to operations that exist arrive with their issue.

All paths are under `/api/v1`. Every operation needs somebody signed in.
"view", "edit" and "administer" are the project rights of #12; a requirement,
project or baseline the caller may not view is 404, never 403, and a change in
an archived project is 409 `project_archived`. A refusal is a sentence that
says what to do, as in M1. Coverage's rules and its three dimensions stay as
#38 left them.

## Migrations, packages and settings

| Range | Owner | Holds |
|---|---|---|
| 00770 to 00779 | trace model (#151) | `trace_level` (a project's levels in order), `trace_level_type` (a requirement level's types), `trace_level_step` (the link types or relations that lead to a level from the one before), their policies and guard |
| 00780 to 00789 | matrix views (#152) | `saved_view.kind` and `saved_view.settings`, the name unique per owner, project and kind |
| 00790 to 00799 | baseline snapshot (#153) | `baseline.detail`, `baseline_item.type_id`, `baseline_link.suspect`, `baseline_external_link.status_category` and `suspect`, `baseline_page_link`, and the guards of 00550 over them |

New package: `internal/xlsx` (#154), a workbook written with `archive/zip`
and `encoding/xml` as `internal/docx` writes Word. The trace model and the
chain report go into `internal/trace` (`chain.go`), the baseline's matrix into
`internal/baseline` (`matrix.go`). No new settings, events or worker topics.
The ranges follow the order the issues are built in, so goose never meets a
lower number after a higher one.

New audit action: `trace_model.saved`.

## The matrix as #38 left it, and what changes

`GET /projects/{projectKey}/matrix` and `matrix.csv` keep every parameter and
field they have. Each issue below adds to them; a client that sends none of
the new parameters gets #38's matrix, with more fields in it.

## #147 Axes, link types and filters

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/matrix` | view | `{matrix}`; 400 `bad_query`; 422 |
| `GET /projects/{projectKey}/matrix.csv` | view | a CSV file; 400 `bad_query`; 422 |

- **New parameters**, each optional:
  - `linkType`, repeated: keys of the link types to count, of those the
    project has enabled; every type when absent. An unknown key is 422 on
    `linkType`.
  - `direction`: `outgoing` (the row is the link's source), `incoming` (the
    row is its target) or `both`, the default. A type that reads the same
    both ways counts in either. Direction concerns requirement columns only.
  - `relation`, repeated: the Armature relations (`implements`, `verifies`,
    `relates`) of issue columns, and from #150 the Stator relations of page
    columns; every relation when absent. One that does not fit the columns
    is 422 on `relation`.
- **Rows** follow `sort` and `desc`, and the query's `ORDER BY` above both,
  as the grid's do; outline order when none is given.
- **Each row** is the requirement as before, plus `canLink`: whether the
  reader may make a link from it, as `POST /requirements/{reqKey}/links`
  would decide (they edit it, it is not in the trash, the project is open).
- **Each link of a cell** gains `id`, `kind` (`trace` or `armature`, and
  `stator` from #150), `typeKey` for a trace link, and `direction`, which
  way it points from its row. `name` and `suspect` stay.
- **UI.** The matrix moves to its own page, `/p/$projectKey/matrix`, which
  Coverage links to. Its toolbar has the grid's filters and query for the
  rows, the link types, the direction, the columns and their project, and the
  relations. The whole setup is the page's address, so a link shares it and a
  reload keeps it, and the download carries it.

- **As built** (#147):
  - The matrix moved to `internal/trace/matrix.go` and takes a
    `trace.MatrixQuery`. `search.Keep` takes the grid's order, sharing
    `orderBy` with the grid, and reads whether the reader edits each row.
  - A link type is any key the installation or either axis's project has,
    turned off for the project or not: links of a type turned off stay, and
    the matrix still shows them. Link types with issue columns, and
    relations with requirement columns, are 422 on their field rather than
    ignored, so a setup never narrows by something it cannot.
  - Relations are checked against Armature's in the handler, since
    `internal/trace` sits below `internal/armature`.
  - The web page's address holds the grid's narrowing and order, `columns`
    only when it is `issues`, and only what fits those columns; the link
    types and relations are pressed chips, none pressed counting all.

## #148 Gaps

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/matrix` | view | `{matrix}`; 400 `bad_query`; 422 |
| `GET /projects/{projectKey}/matrix.csv` | view | a CSV file; 400 `bad_query`; 422 |

- **A column filter.** `columnQuery` (the query language, over the column
  project) and `columnType`, repeated, make the columns the requirements
  they keep, in the column project's outline order, linked or not. Without
  them the columns are what the rows reach, as in #38. They are for
  requirement columns: with issue or page columns they are 422 on the field.
- **Columns are paged** as rows are: `columnLimit`, 1 to
  `trace.MatrixMaxColumns` (500), 100 when absent, and `columnOffset`. The
  answer adds `columnTotal`, `columnLimit` and `columnOffset`, and a page
  never refuses for having too many columns. The file holds every column, and
  refuses more than 500 with 422 asking for a narrower column filter.
- **Counts.** `rowLinks` and `columnLinks`, in the order of `rows` and
  `columns`, count each one's links over every column, or every row, the
  filters keep, not only the page's.
- **Only gaps.** `gaps=rows` keeps the rows with no link to any column kept,
  `gaps=columns` the columns with no link from any row kept, `gaps=both`
  both; `total` and `columnTotal` count what is kept. `columns` and `both`
  need a column filter, since columns that rows reach are never empty;
  without one they are 422 on `gaps`.
- **The file** follows the same setup, and adds a line under the header with
  each column's count, and a column after the title with each row's.
- **UI.** A column filter beside the rows', the counts at the end of each row
  and the foot of each column, empty ones marked, and an "only gaps" choice.

- **As built** (#148):
  - The matrix reads every link between the rows kept and the columns it
    may show in one query, `internal/trace/matrix_layout.go`, and pages
    both after counting, so the counts and gaps cover the whole matrix.
    Without a column filter the columns may be any requirement of the
    column project, shown when the page's rows reach them.
  - A refused column query is 400 `bad_query` with its position and
    `fields.columnQuery`, so the page shows it under the column filter's
    box. A refused `columnType` is 422 on `columnType`.
  - The file holds every column, refusing more than 500 with 422 asking for
    a narrower filter, and its second line and third column are the counts.
  - On the page, the column query offers words of the column project, the
    column types are chips, and "Only gaps" offers columns and both only
    once a column filter is set; the address drops them otherwise.

## #149 Links made, removed and cleared in a cell

No new operations: a cell is a place to call the ones a requirement page
calls, with the same rights.

| What | Operation |
|---|---|
| list a row's links, for their revisions | `GET /requirements/{reqKey}/links`, `/armature-links`, `/stator-links` |
| link a row to a requirement column | `POST /requirements/{reqKey}/links` with the column's key, a type and a direction |
| link a row to an issue column | `POST /requirements/{reqKey}/armature-links` with the issue key and a relation |
| link a row to a page column (from #150) | `POST /requirements/{reqKey}/stator-links` with the page id and a relation |
| remove a link | `DELETE /links/{linkID}`, `/armature-links/{linkID}`, `/stator-links/{linkID}` |
| clear a suspect link | `POST /links/{linkID}/clear`, `/armature-links/{linkID}/clear`, `/stator-links/{linkID}/clear` |

- **The panel** opens anchored to its cell, lists the cell's links with
  their type, direction and suspect state, and offers removing and clearing
  each, and adding one, to whoever the row's `canLink` allows. Anybody else
  reads it. Escape closes it and gives focus back to the cell.
- **Adding** offers the project's enabled link types, each forward and, where
  it reads differently, backward, as the requirement page does; issue and
  page columns offer their relations.
- **After a change** the matrix is read again at once. Coverage already
  forgets on the events a link makes, removes or clears; the web client also
  drops its own copy of the project's coverage.

- **As built** (#149):
  - Each cell is a button wherever it has a link, or its row may be linked;
    an empty cell of a row the reader may not link is no button.
  - The panel is `features/matrix/CellPanel.tsx`, on the overlay hooks the
    bell uses. It shows the cell's links as the matrix counted them, read
    from the row's own links for their revisions and suspect ends, and adds,
    removes and clears through the requirement page's hooks.
  - `useAnchored` now places a panel again whenever it resizes, so one that
    loads what it shows turns to the side with room once it has.
  - The link and Armature link hooks, and clearing, also drop the cached
    coverage and matrix, so the cell shows a change at once.

## #150 Stator pages as columns

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/matrix?columns=pages` | view | `{matrix}`; 422 |
| `GET /projects/{projectKey}/matrix.csv?columns=pages` | view | a CSV file; 422 |

- **Columns** are the Stator pages the rows link to, neither broken nor
  detached, ordered by space and then title as last seen, then id. A column
  is `{page: {id, spaceKey, url}}`; `requirement` is null and `issueKey`
  empty.
- **Each link** is `kind` `stator`, its `name` the relation (`specifies`,
  `describes`, `relates`), and suspect by Stator's rule: the page has a
  version past the one reviewed, or the requirement a revision past it.
- **Titles** are Stator's to tell, as the reader sees them: the web client
  asks `GET /stator/pages`, and the file asks Stator as the reader the same
  way. A page the reader cannot read there is named by its space and id.
- Coverage does not count pages.

- **As built** (#150):
  - Exciter keeps no page's title, so columns are ordered by space, then
    page id, not by title.
  - A page link is suspect by the rule the requirement page shows: its page
    has a version past the one confirmed. That rule,
    `trace.StatorSuspectSQL`, now lives below `internal/stator`, which
    names it `stator.SuspectSQL`, so the two never drift.
  - Relations are checked against Stator's for page columns, and
    Armature's for issue columns; link types and a column filter are 422
    with any columns but requirements.
  - The file names a page by the title `GET /stator/pages` gives the reader,
    or by `SPACE/id` where Stator gives none; the web client asks the same
    lookup and caches it per batch. A page cell's panel traces and confirms
    through the Stator link hooks.

## #151 The trace model and the chain report

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/trace-model` | view | `{model}` |
| `PUT /projects/{projectKey}/trace-model` | administer | `{model}`; 403; 409 `project_archived`; 422 |
| `GET /projects/{projectKey}/trace-chains` | view | `{chains}`; 400 `bad_query`; 422 |
| `GET /projects/{projectKey}/trace-chains.csv` | view | a CSV file; 400 `bad_query`; 422 |

- **The model** is a project's levels, first to last, at most
  `trace.MaxLevels` (6). A level has a name (1 to 40 characters, unique in
  the model ignoring case) and a kind:
  - `requirements`: one or more of the project's own requirement types. A
    level never reaches into another project; comparisons across projects
    stay the matrix's `columnProject`.
  - `issues`: the Armature issues the level before is linked to.
  - `pages`: the Stator pages the level before is linked to.

  Every level but the first has steps, the ways down from the level before:
  for a requirement level, link types each with a direction, read as the
  link's phrase from the upper level (`verifiedBy` from System to Test is
  `verifies`, `incoming`); for an issue or page level, relations. The first
  level is a requirement level without steps. Issues and pages end a model:
  nothing follows them.
- **Saving** replaces the whole model; no levels clears it. A project starts
  without one, and nothing is installation-wide. A level naming a retired
  type, another project's type or a link type the project has not enabled,
  or a step on the first level, is 422 on the field. Saving is audited as
  `trace_model.saved`. The database refuses a model written by somebody who
  does not administer the project, or naming another project's types,
  straight through SQL.
- **The chain report** pages the roots: the first level's requirements the
  grid's filters keep, in the order the grid would give them. From each root
  it follows the steps level by level, as the reader sees the links, and
  answers one line per path: a node per level reached, each saying what it
  is, the link name that led to it and whether that link is suspect. A line
  with fewer nodes than levels shown breaks below its last node; a root with
  nothing below it is one line of one node. A requirement in the trash, a
  broken or detached Armature or Stator link, and a link the reader cannot
  see, lead nowhere.
- **Parameters.** `level`, repeated, shows a subset of the levels by
  position, the first one shown being the roots; `onlyBroken=true` keeps the
  lines that break. `broken` counts the broken lines of the page.
- **Limits.** A page answers at most `trace.MaxChainLines` (2000) lines;
  more is 422 asking for fewer roots per page or a narrower filter. A model
  without levels answers no lines, and the page tells administrators where
  to set one.
- **The file** has a column per level shown, a node's key and title, or
  issue key, or page title, in each, a suspect link marked `(suspect)`, and a
  last column saying where a broken line breaks. It holds every root the
  filter keeps up to the export's 20000 lines, and says so on its last line
  when there were more.
- **UI.** A Trace model card in the project's settings, levels added,
  reordered and removed in place, read-only for anybody who does not
  administer the project; and a Chains view beside the matrix, its levels,
  filters and "only broken" in the page's address.

## #152 Matrix and chain views

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/views?kind=` | view | `{views}` |
| `POST /projects/{projectKey}/views` | view; edit to share | 201 `{view}`; 400; 422 |
| `PATCH /views/{viewID}` | the owner; administer once shared | `{view}`; 400; 422 |
| `DELETE /views/{viewID}` | the owner; administer once shared | 204 |

- **A view has a kind**: `grid`, the default, `matrix` or `chain`. Lists
  answer one kind, `grid` when `kind` is absent, so the grid's list is what
  it was. A name is unique per owner, project and kind.
- **Settings.** A grid view keeps `filter`, `columns` and `sort`. A matrix
  or chain view keeps `filter` and `sort` for its rows, and `settings`: for a
  matrix, `columns`, `columnProject`, `linkTypes`, `direction`, `relations`,
  `columnFilter` and `gaps`; for a chain view, `levels` and `onlyBroken`.
  Settings that do not fit the kind are 422 on the field; `columns` stays
  empty for a matrix or chain view.
- **Sharing, changing and deleting** are as for grid views (#20).
- **What the project no longer has.** A view naming a link type, a type or a
  level the project has since lost opens without it, and the page says in a
  sentence what was left out.
- **UI.** The grid's views control, given a kind, on the matrix and the chain
  report; `?view=` opens one.

## #153 A baseline's matrix

| Operation | Needs | Answers |
|---|---|---|
| `GET /baselines/{baselineID}/matrix` | view | `{matrix}`; 422 |
| `GET /baselines/{baselineID}/matrix.csv` | view | a CSV file; 422 |

- **What baselines record from now on.** As well as what they keep, a
  baseline taken after this records each item's requirement type, whether
  each trace link was suspect, each Armature link's status category and
  whether it was suspect, and the Stator page links with their relation and
  whether they were suspect. Broken and detached links are no longer
  recorded. Its `detail` is `2`; one taken before is `1`.
- **The matrix** lays a baseline's requirements, in its outline, against the
  requirements (`columns=requirements`, the baseline's own), Armature issues
  (`issues`) or Stator pages (`pages`) they were linked to, with `linkType`
  and `direction` as for the project's matrix. A link's `suspect` and
  `status` are null where the baseline did not record them, and page columns
  of a detail 1 baseline are empty.
- **Compared** with `with`, another baseline of the project or `now`, every
  link one side has and the other has not is in its cell with `change`
  `added` or `removed`, and a link both have whose suspect state or status
  differs is `changed`. Rows and columns are those of either side. `with`
  names the other side as the comparison does (#37).
- **The file** follows the same setup, marking changed links `(added)`,
  `(removed)` or `(changed)`.
- **UI.** A Matrix tab on the baseline's page, with its "compare with"
  choice, cells coloured by their change, and "not recorded" where the
  baseline did not record what the page shows.

## #154 Excel

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/matrix.xlsx` | view | an xlsx file; 400 `bad_query`; 422 |
| `GET /projects/{projectKey}/trace-chains.xlsx` | view | an xlsx file; 400 `bad_query`; 422 |
| `GET /baselines/{baselineID}/matrix.xlsx` | view | an xlsx file; 422 |

- **Each takes the parameters of its CSV** and holds what the CSV holds.
- **The workbook** has one sheet; a bold header row and the key column
  frozen; column widths from their content within bounds; an autofilter over
  the header; linked cells green, suspect ones amber, empty rows and columns
  of a gap view and a chain's break red, a baseline's added, removed and
  changed links in their own colours; and the note on its last row when rows
  were cut, with `X-Export-Truncated`.
- **Built in memory** before the first byte is written, so a refusal is
  still the JSON error. Written without a new dependency.
- **UI.** An Excel download beside each CSV one.
