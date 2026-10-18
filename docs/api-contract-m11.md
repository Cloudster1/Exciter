# API contract for M11: Analysis and reporting

M11 helps people see what a specification holds and take it elsewhere:
- a graph shows everything a requirement traces to, up and down, with
  suspect and broken links and coverage gaps marked, and lists what a
  change to it would affect
- attributes compute themselves from a formula over a requirement's own
  fields, its children or its linked requirements
- dashboards of charts and counters over a project's requirements, read
  live as the reader, open their requirements in the grid
- a specification is exported through an organisation's own Word template,
  and as a static HTML site that opens offline

The epic is #241; this file is #242. The route table in
`backend/internal/httpapi/operations.go` and `operations_m11.go` is the
source of truth for paths, fields and types, and this file says what they
mean. Operations it adds carry `pending: true` and answer 501 until their
issue is built. Parameters and fields it adds to operations that exist
arrive with their issue.

All paths are under `/api/v1`. Every operation needs somebody signed in or a
token. "view", "edit" and "administer" are the project rights of #12. A
project, requirement, dashboard or template the caller may not view is 404,
never 403, and a change in an archived project is 409 `project_archived`. A
refusal is a sentence that says what to do, as in M1.

## Migrations, packages and settings

- Migration ranges, in build order:
  - 01100 to 01119 for the trace graph (#211)
  - 01120 to 01139 for computed attributes (#213)
  - 01140 to 01159 for dashboards (#212)
  - 01160 to 01179 for Word templates and the HTML site (#214)
- Packages:
  - `internal/trace` gains the graph (#211) beside the chain report.
  - `internal/attribute/formula` (#213): the formula language, its parser,
    checker and evaluator, in the style of `internal/rql`. It reads neither
    the database nor `internal/attribute`, which calls it.
  - `internal/attribute` keeps computed values and recomputes them in the
    worker (#213).
  - `internal/dashboard` (#212): dashboards and widgets, adapted from
    Armature's `internal/report`. It sits above `internal/search`,
    `internal/trace` and `internal/quality`, which it reads.
  - `internal/exporttemplate` (#214): Word templates. `internal/docx` gains
    a reader of a template and writes through one; `internal/sitezip`
    builds the HTML site.
- Settings: none in the environment. A project names its home dashboard.
  The render service gains an HTML answer beside its PDF one (#214).
- Limits:
  - `trace.GraphMaxDepth` = 6 levels, `trace.GraphDefaultDepth` = 2,
    `trace.MaxGraphNodes` = 500.
  - `formula.MaxLength` = 2000 characters, `formula.MaxDepth` = 32,
    `formula.MaxSteps` = 64: how often one value is worked out again while
    what it reads keeps changing.
  - `attribute.RecomputeBatch` = 200: requirements one transaction of a
    recompute takes.
  - `dashboard.MaxDashboards` = 50 per person and project, shared ones
    included; `dashboard.MaxWidgets` = 30; `dashboard.MaxWidth` = 3
    columns; `dashboard.MaxNameLength` and `MaxTitleLength` = 80.
  - `dashboard.MaxBuckets` = 20 slices of a breakdown, the rest counted as
    one; `dashboard.MaxListRows` = 50, 10 by default;
    `dashboard.MaxDays` = 730 for a widget over time, 90 by default.
  - `exporttemplate.MaxFileMB` = 20, `exporttemplate.MaxUnpackedMB` = 64;
    `exporttemplate.MaxTemplates` = 50 per project;
    `exporttemplate.MaxFields` = 60.
  - An HTML site holds at most `specdoc.MaxEntries` entries, as a document
    does.
- **Decided** (`docs/decisions.md`, 2026-10-18):
  - up in the graph is towards what a requirement derives from
  - computed values are kept outside revisions, and recomputed from a queue
    the database fills
  - roll-ups read the requirement's own project only
  - dashboards are shared with the project's readers, never by a public link
  - a Word template is a frame around the export
  - the HTML site is taken from the print view

## #211 The trace graph

| Operation | Needs | Answers |
|---|---|---|
| `GET /requirements/{reqKey}/graph` | view | `{graph}`; 400 |
| `GET /requirements/{reqKey}/graph.csv` | view | a CSV file; 400 |

### The walk

- **Centred on a requirement**, the graph follows links `up`, `down` or
  `both`, to `depth` levels, 1 to `GraphMaxDepth`, 2 when absent.
- **Up** follows the links a node is the source of: it derives from,
  refines, satisfies or verifies what is up. **Down** follows the links a
  node is the target of. A type whose two phrases are the same, such as
  relates, is followed both ways.
- **Below a requirement**, going down, are the Armature issues and Stator
  pages linked to it, the tests whose latest results name it (#200) and the
  places in the code that tag it (#201). They are leaves: the walk does not
  go on from them.
- **What is followed:** `type` names link types by key, every type the
  project uses when absent; `relation` names the relations of issues,
  pages, tests and code, every one when absent. A key or relation that
  does not exist is 400.
- **Across projects.** A link to a requirement of another project is
  followed when the reader may view it there. A test's or a place's
  evidence is read on its own project's counting branch; `branch` reads
  another for the centre's project only.
- **Each node once**, at the level it was first reached; a link back to a
  node already reached is an edge, not a new node. The walk stops at
  `MaxGraphNodes` and says `truncated`.

### Marks

- **Suspect**: an edge whose link is suspect (#36), or a Stator page newer
  than its link saw, or an issue linked to an older revision.
- **Broken**: a node whose issue or page was deleted or unlinked on its
  side, or a requirement in the trash, and the edge to it. The chain report
  leaves these out; the graph shows them, because a change that reaches
  them reaches nothing.
- **Gaps**: on a requirement node, the coverage dimensions its project's
  rules ask of it (#148) and find missing or partial.
- **Tests and code**: a test that failed or errored is failing; a test or
  tag that names an older revision than the current one is outdated.

### What the reader may not see

A requirement the reader may not view is never a node and never named.
Each node says in `hidden` how many of its links, of the types followed,
lead to such requirements, and the graph sums them. A link from a hidden
requirement to one the reader may see does not lead the walk on.

### The impact list

`graph.csv` writes every node, nearest first: its level, kind, key or
name, title, type, status, the link that reached it, and its marks. Each
cell passes the guard against formulas, as every CSV export does.

### Guards

The hidden counts come from a database function that counts for the person
asking alone, around requirements they may view; a requirement in the
trash is not counted. It is proven straight through SQL as `exciter_app`.

### MCP

`get_trace_graph` reads `GET /requirements/{reqKey}/graph`, so an assistant
says what a change affects before it is made.

### UI

- A requirement's page links to its graph, `/p/{projectKey}/r/{reqKey}/trace`:
  up above the requirement, down below it, each node with its key, title,
  type and status, and marks for suspect, broken, failing and outdated.
- Choosing a requirement node centres the graph on it; "Open" goes to the
  item, in its own project, in Armature or in Stator.
- Controls set the direction, the depth, the link types and the relations.
  Beside the graph, the impact list names everything reached by level and
  downloads as CSV.
- Every node is reached by keyboard, and "List" shows the same graph as a
  nested list for a screen reader.

### As built (#211)

- **Migration**: `01100_trace_graph.sql` adds `trace_hidden_links(ids,
  directions, types)`, SECURITY DEFINER, counting for `current_actor_id()`
  alone and only around requirements that person may view.
- **Code**: `trace/graph_walk.go` walks level by level behind a small
  reader, so the walk is tested over a fixture; `trace/graph_service.go`
  reads the steps, the gaps (the coverage judge per project) and the
  hidden counts; `csvio/impact.go` writes the CSV.
- **Refusals**: a direction, depth, link type or relation that cannot be
  walked is 400 `bad_request` with the field named.
- **Evidence branch**: `branch` reads the centre's project's tests and code
  on that branch, and judges that project's gaps on it too; every other
  project's on its counting branch.
- **Web**: `features/trace`, at `/p/{projectKey}/r/{reqKey}/trace`, its
  search holding the direction, depth, types, relations and the list view.
  React Flow pans, zooms by buttons and walks the nodes by keyboard; the
  layout is ours, a row per level.

## #213 Computed attributes

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/formula-check` | administer | `{check}`; 403; 422 |

The attribute operations of #14 gain, on `Def` and `DefInput`:
- `formula`: the formula, kept with the names of attributes and link types
  as they are now spelled; null for an attribute people type
- `onSections`: whether it is worked out for sections too
- on `GET /projects/{projectKey}/attributes`, `recompute`:
  `{done, total, startedAt}` while the project is recomputing, null
  otherwise

### Formulas

- **A value of four kinds**: `number`, `text`, `boolean` or `date`. The kind
  follows from the formula and is fixed with it.
- **Quotes**: an attribute is named in double quotes, `"Weight"`; text is
  written in single quotes, `'approved'`.
- **A requirement's own fields**: `title`, `status`, `category` (of the
  status), `type`, `owner`, `priority`, `revision`, `created`, `updated`,
  `quality` (#207) and `testResult` (`passed`, `failed` or `none` on the
  counting branch, #200), and its attributes by name in double quotes,
  `"Weight"`, computed ones included.
- **Operators**: `+ - * /`, `= != < <= > >=`, `and`, `or`, `not`, and
  parentheses. Text is joined with `concat`.
- **Functions**: `if(condition, then, else)`, `coalesce`, `round`, `floor`,
  `ceil`, `abs`, `min`, `max`, `concat`, `lower`, `upper`, `len`, `today()`
  and `days(from, to)`.
- **Aggregates** read many requirements: `count`, `sum`, `min`, `max`,
  `avg`, `any` and `all`, over `children` or over
  `links("<link type key>", out)` or `links(..., in)`, with an optional
  condition: `count(children where category = 'approved') / count(children)`.
  `out` follows the links the requirement is the source of. A link type is
  named by its key, never by its phrase, which can be the same both ways
  and can change.
- **The requirement's own project only.** Children are always in it; a
  link to a requirement of another project is not read, so a value never
  counts what a reader of the project may not see. Requirements in the
  trash are not read.
- **A missing value** makes an arithmetic result null; `coalesce` gives a
  default. Dividing by zero is null.
- **Kept by id.** The checked formula names attributes and link types by
  id, so renaming either keeps it working; it is shown with the names as
  they are now.

### Defining one

- A computed attribute is made with a formula and keeps it: an attribute
  people type never becomes computed, and a computed one never stops being
  computed. Its formula may change.
- It shows on the types it names, as any attribute, and on sections when
  `onSections` is set.
- It is never required, never depends on another, offers no choices and
  names nobody who may edit it: 422 on each.
- **Cycles are refused when saved**, with the cycle named: 422 on
  `formula`, "Score reads Weight, which reads Score. Take one of them out
  of the other's formula." Reading itself through `children` is allowed,
  since the tree has no cycles; through `links` only over an acyclic link
  type. Turning `acyclic` off on a type such a formula follows is 409
  `type_in_formula`, naming the attribute.
- An attribute a formula reads cannot be retired or deleted: 409
  `attribute_in_formula`, naming the formula's attribute.
- `formula-check` answers what is wrong and where, as the query language
  does, the kind of value, what it reads, and, given `requirementKey`, its
  value for that requirement.

### Values

- **Not revisions.** A computed value is kept beside the requirement, not
  in its attributes: it never writes a revision, never changes the content
  a signature covers, and is never locked.
- **Read like any attribute**: in the requirement, the grid, as a column,
  in the query language by name, ordered and filtered by kind, and in every
  export. It is read-only in the editor, marked "Computed".
- **A value that cannot be had** is null, with the reason kept beside it:
  a cycle that older links make, or `MaxSteps` reached.
- **Imports**: CSV refuses a column of a computed attribute, and ReqIF
  leaves one out with a loss. A move or copy to another project never
  carries a computed value as a typed one.

### Recomputing

- **What a value reads changes it.** The database marks a requirement to
  recompute in the transaction that changes what its formulas read: its
  own fields and attributes, a child added, moved away, trashed or
  restored, a link made or removed, a test result, a finding accepted.
- **The worker recomputes** what is marked, project by project, at most
  `RecomputeBatch` per transaction. A value that changes marks what reads
  it: the parent, and the requirements whose links reach it.
- **Changing a formula** marks the whole project; the attributes show
  `recompute` with how far it has got until it is done.
- **A baseline** records the values it saw, as they were when it was taken,
  and whether a recompute was running.

### Guards

- Only the worker writes computed values; nobody else may, the API's role
  included.
- A computed attribute's id is refused in a requirement's typed
  attributes.
- A formula is fixed as computed at creation, and its attribute cannot
  become required.
- A baseline's computed values are only ever added to, by the transaction
  that takes it.

All of it is held by the database and proven straight through SQL as
`exciter_app`.

### Refusals

| Status | Code | When |
|---|---|---|
| 403 | `forbidden` | a formula checked or saved without administer |
| 409 | `attribute_in_formula` | retiring or deleting an attribute a formula reads |
| 409 | `type_in_formula` | turning `acyclic` off on a link type a self-reading formula follows |
| 422 | `validation_failed` | a formula that does not parse or check, a cycle, a computed attribute made required, dependent, with choices or editors |

### MCP

The attribute tools answer computed values as they answer typed ones; no
new tool.

### UI

- An attribute's settings gain "Computed", with a formula editor that
  completes fields, attributes, link types and functions, marks what is
  wrong where it is, and tries the formula on a requirement.
- A line says how far a recompute has got.
- The editor shows computed values read-only, marked "Computed".

### As built (#213)

- **Migration**: `01120_computed.sql` adds:
  - `attribute_def.formula` (kept with `"@id"` and `'@id'` for attributes
    and link types) and `on_sections`, and the trigger that keeps an
    attribute computed or typed as it was made
  - `requirement_computed`, written by the worker alone;
    `requirement_computed_of(id)` reads it as the reader sees it
  - `computed_dirty`, the queue the triggers on `requirement`,
    `trace_link`, `test_result` and `quality_acceptance` fill, only in
    projects with computed attributes; `computed_mark`,
    `computed_recompute`, `computed_progress` and `computed_pending`. Since
    `01121_computed_queue.sql` (#248) marks are only appended, with no
    unique or foreign key, so a change never waits on the worker; the
    worker takes a requirement's marks together, with the fewest steps
  - `attribute_recompute`, how far a recompute after a formula changed has
    got
  - `baseline_computed` and `baseline.computed_stale`, with
    `baseline_content_hash` and `baseline_content_rows` covering the new
    lines after the others, so a baseline without them keeps its hash
- **Code**: `attribute/formula` parses, checks, evaluates and finds cycles;
  `attribute/compute.go` reads what formulas read; `attribute/recompute.go`
  is the worker's loop, a batch of `RecomputeBatch` per transaction under a
  lock per project. A value changing marks the parent and the requirements
  whose links lead to it; a requirement's own change always does, since a
  roll-up may read its typed values.
- **Reading**: the query language and the grid read a computed attribute
  from `requirement_computed_of(r.id)`; grid rows, documents, CSV and ReqIF
  exports read typed and computed values together. A requirement answers
  them apart, as `computed`.
- **Imports and moves**: CSV and ReqIF imports, and a move or copy to
  another project, leave computed attributes out; a CSV column mapped to one
  is refused as no target.

## #212 Dashboards

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/dashboards` | view | `{dashboards}` |
| `POST /projects/{projectKey}/dashboards` | view; edit to share | 201 `{dashboard}`; 403; 409 `too_many_dashboards`; 422 |
| `GET /dashboards/{dashboardID}` | its owner, or view once shared | `{dashboard}` |
| `PATCH /dashboards/{dashboardID}` | its owner, or administer once shared | `{dashboard}`; 403; 422 |
| `DELETE /dashboards/{dashboardID}` | the same | 204; 403 |
| `POST /dashboards/{dashboardID}/widgets` | the same | 201 `{widget}`; 403; 409 `too_many_widgets`; 422 |
| `PUT /dashboards/{dashboardID}/order` | the same | `{dashboard}`; 403; 422 |
| `PATCH /widgets/{widgetID}` | the same, of its dashboard | `{widget}`; 403; 422 |
| `DELETE /widgets/{widgetID}` | the same, of its dashboard | 204; 403 |
| `GET /widgets/{widgetID}/data` | whoever may read its dashboard | `{data}`; 422 |
| `POST /projects/{projectKey}/widget-preview` | view | `{data}`; 422 |
| `GET /dashboard-templates` | signed in | `{templates, kinds}` |
| `GET /dashboards/{dashboardID}/dashboard.pdf` | whoever may read it | a PDF; 503 `render_unavailable` |

### Dashboards

- **A dashboard belongs to a project and a person.** It is theirs alone
  until shared; shared, everybody who may view the project reads it.
  Sharing needs edit on the project. Once shared, its owner and the
  project's administrators change it; its owner alone otherwise.
- **Shared by link.** A shared dashboard's address is its link: a reader
  of the project opens it, anybody else is told it does not exist. There
  is no public link.
- **A name** of at most `MaxNameLength` characters; two dashboards of one
  owner in a project never share one.
- **Templates** give a first dashboard: `overview` (status, coverage,
  review, suspects, quality and changes) and `verification` (coverage,
  failing and outdated tests, what is not verified).
- **The project's home** shows a dashboard its administrators choose:
  `project.settings` gains `homeDashboardId`, set through
  `PATCH /projects/{projectKey}`. It must be a shared dashboard of the
  project: 422 on `homeDashboardId` otherwise. Deleting it or taking its
  sharing away clears it.
- Making, sharing and deleting a shared dashboard is audited as
  `dashboard.created`, `dashboard.shared` and `dashboard.deleted`.

### Widgets

- **A source**: a query in the query language (#47), or a shared saved
  view of the grid (#20), whose filter it reads; the project's every
  requirement when neither. A private view is 422 on `viewId`, "Share the
  view first, or write its query here."
- **Read live as the reader**: a widget counts only what the reader may
  see, as the grid does, every time it is read. Nothing is kept.
- **Kinds**:

| Kind | Shows | A bucket opens |
|---|---|---|
| `breakdown` | counts by status, category, type, owner, priority, label or an attribute (computed ones included), as bars or a donut | the requirements with that value |
| `count` | how many the source keeps | all of them |
| `list` | the first `rows`, in the source's order, with the grid's `columns` | the requirement |
| `coverage` | full, partial and none per dimension | `verified = none` and the like |
| `review` | approved, waiting, outdated and never reviewed | `approval = pending` and the like |
| `suspects` | how many have suspect links, and those with most | `suspect > 0` |
| `changes` | created, revised and moved to another status, per week or month over `days` | `created >= ... AND created < ...` and the like |
| `quality` | how many score in each band, and the average (#207) | `quality >= 60 AND quality < 80` |

- **Into the grid.** Each bucket carries `query`, a condition in the query
  language; the page opens the grid with the source's query and the
  bucket's joined by `AND`, and the view for a view's source.
- **Laid out** in order, `width` 1 to `MaxWidth` columns, by dragging or
  by the buttons that move a widget and change its width.
- A widget the reader may read but whose source no longer compiles
  answers 422 `bad_query` with the query language's sentence.

### The PDF

`dashboard.pdf` prints the dashboard by the render service as the reader,
as the specification is printed (#45): what the reader sees is what is
printed.

### Guards

- A dashboard is read by its owner, or once shared by readers of its
  project; it is changed by its owner, or once shared by the project's
  administrators; sharing it needs edit.
- A widget belongs to one dashboard and moves with nothing else.

All of it is held by the database and proven straight through SQL as
`exciter_app`.

### Refusals

| Status | Code | When |
|---|---|---|
| 403 | `forbidden` | sharing without edit; changing a shared dashboard without administer |
| 409 | `too_many_dashboards` | more than `MaxDashboards` in a project |
| 409 | `too_many_widgets` | more than `MaxWidgets` on a dashboard |
| 422 | `validation_failed` | a name empty, too long or taken, an unknown kind, group, shape, series or attribute, a width out of range, a private view, an order that does not name every widget once |
| 422 | `bad_query` | a source whose query does not compile |

### MCP

- `list_dashboards` reads `GET /projects/{projectKey}/dashboards`.
- `get_dashboard_widget` reads `GET /widgets/{widgetID}/data`, so an
  assistant answers from a dashboard the reader may read.

### UI

- The project's navigation gains Dashboards: the reader's own and the
  shared ones, "New dashboard" from a template, and "Copy link".
- A widget is added by kind, its source chosen as a query, with
  completion, or a view, and previewed before it is saved.
- Every chart is drawn without a library, as Armature's are, and has "As a
  table". A slice or a bar opens its requirements in the grid.
- The home of a project shows its home dashboard above its requirements.
- "Export as PDF" prints the dashboard.

### As built (#212)

- **Migration**: `01140_dashboards.sql` adds `dashboard` and
  `dashboard_widget`, guarded by `dashboard_kept(id)`, and
  `project_clear_home_dashboard(project)`, which clears a project's home once
  its dashboard is gone or no longer shared.
- **Code**: `internal/dashboard` keeps dashboards and widgets
  (`service.go`, `widgets.go`), reads widgets (`data.go`) through
  `search.Keep`, `search.Grid`, `search.Quality` and `trace.Coverage` as the
  reader, and holds the built-in templates in code.
- **Buckets**: breakdowns group in SQL over the requirements the source
  keeps; review and suspects count each state through the query language,
  so a bucket's count is the grid's for its query. A bucket with no value
  has an empty label and an `IS EMPTY` query; the tail past `MaxBuckets` has
  an empty label and no query.
- **Refusals**: a source whose query does not compile is 400 `bad_query`, as
  the grid answers it.
- **Web**: `features/dashboard`; `/p/{projectKey}/dashboards` and
  `/p/{projectKey}/dashboards/{id}`; `/print/dashboards/{id}` for the PDF;
  the home dashboard is chosen with the project's details and shown above
  its requirements.

## #214 Word templates and the static HTML site

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/export-templates` | view | `{templates}` |
| `POST /projects/{projectKey}/export-templates` | administer | 201 `{template}`; 403; 409 `too_many_templates`; 413; 422 |
| `GET /export-templates/{templateID}` | view of its project, or any project once shared | `{template}` |
| `PATCH /export-templates/{templateID}` | administer of its project | `{template}`; 403; 422 |
| `DELETE /export-templates/{templateID}` | administer of its project | 204; 403 |
| `GET /export-templates/{templateID}/file` | as reading it | the file |
| `GET /projects/{projectKey}/document.html.zip` | view | a zip; 422; 503 `render_unavailable` |
| `GET /baselines/{baselineID}/document.html.zip` | view | a zip; 422; 503 `render_unavailable` |

The exports of #45 and #204 gain `template`: `document.docx` and
`document.pdf` of a project and of a baseline, and the review session's
`report.docx`. A template the project may not use is 422 on `template`.
With a template whose `include.changeMarks` is set, `since` names a
baseline of the project the changes are counted from.

### Templates

- **A template belongs to a project**, uploaded by its administrators, at
  most `MaxTemplates`. **Shared**, it is offered in every project; only its
  own project's administrators change or delete it. Uploading, changing,
  sharing and deleting are audited as `export_template.uploaded`,
  `export_template.changed` and `export_template.deleted`.
- **A Word file**, `.docx` or `.dotx`, of at most `MaxFileMB`, unpacking
  into at most `MaxUnpackedMB`. Its file is kept in the object store.
- **The frame of the export.** The export takes the template's styles, its
  theme, its headers and footers, and its page size and margins, and puts
  its own contents and requirements where the template's body says
  `{{content}}`, which stands alone in a paragraph. What the body holds
  before it, such as a cover, comes first, and what follows it last.
- **Styles are matched by name**, `heading 1` or `Title`, as Word names
  them in any language, so a German template's headings are the export's.
  A style the template lacks is the export's own.
- **Placeholders** in the body, the headers and the footers are filled:
  `{{project}}`, `{{title}}`, `{{section}}`, `{{baseline}}`, `{{date}}`,
  `{{exportedBy}}` and `{{signers}}`, empty where they do not apply. A
  placeholder may be split across runs, as Word often splits typing.
- **Fields**: the template names, in order, which fields a requirement
  shows: type, status, owner, priority, labels, revision, rationale, and
  attributes by name, since a shared template serves projects whose
  attributes differ. Empty shows them as an export without a template does.
- **Include**: `links`, `comments` (the open threads), and `changeMarks`,
  what changed since the baseline `since` names, as tracked insertions and
  deletions.
- A template's fields and inclusions apply to the PDF and the HTML site as
  well; its styles to Word alone.

### What is refused

A template that cannot be used is refused when uploaded, with what is
wrong, as 422 on `file`:
- not a Word file: "<name> is not a Word document. Save it as .docx or
  .dotx and upload it again."
- macros: "<name> holds macros, which Exciter does not run. Save it as a
  .docx or .dotx without macros."
- no `{{content}}`, or more than one: "Put {{content}} on a paragraph of
  its own where the requirements go."
- `{{content}}` in a table, a text box or a content control: "Move
  {{content}} out of the table, onto a paragraph of its own."
- an unknown placeholder: "{{projekt}} is not a placeholder. Use
  {{project}}, {{title}}, ..."
- too large: 413 `too_large`.

### The static HTML site

- **Taken from the print view.** The render service opens the print view
  of the specification as the reader and answers its HTML with its styles
  inlined and its scripts left out; Exciter splits it into pages.
- **The zip** holds `index.html`, with the cover, the contents and a search
  box; a page per top-level part, its requirements anchored by key; the
  pictures the text shows; and `search.js`, which searches every page's
  titles and text without a server. Links between requirements, trace
  links included, go to their page and anchor. It opens from a folder,
  without a network.
- Exported as the specification is: audited as `specification.exported`
  with the format `html`; a template's fields apply.

### Guards

- A template is written by its project's administrators, and read by its
  project's readers, or by every signed-in person once shared.
- Its file's object is named from the template, and removed when it is
  deleted.

All of it is held by the database and proven straight through SQL as
`exciter_app`.

### Refusals

| Status | Code | When |
|---|---|---|
| 403 | `forbidden` | uploading or changing a template without administer |
| 409 | `too_many_templates` | more than `MaxTemplates` in a project |
| 413 | `too_large` | a file over `MaxFileMB` or unpacking past `MaxUnpackedMB` |
| 422 | `validation_failed` | a file that cannot be used, a name empty or too long, an unknown field or attribute, a template the project may not use, `since` that is not a baseline of the project |
| 503 | `render_unavailable` | the HTML site without the render service |

### MCP

No new tool: an assistant does not download files.

### UI

- The project's settings gain Templates: upload, the fields in order, what
  is included, and "Share with all projects".
- Exporting a specification, a section, a baseline or a review report
  offers the templates the project may use, and "HTML site (zip)".

## What is left for later

- Roll-ups across projects.
- A dashboard-wide filter, public links to a dashboard, and dashboard
  templates people save.
- A graph of a whole section, and nodes for issues and pages linked only
  from another project.
- A template's styles in the PDF.
- Change marks against anything but a baseline.
