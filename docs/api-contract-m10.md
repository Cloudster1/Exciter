# API contract for M10: Authoring quality

M10 helps authors write a specification that is clear and consistent before
it is reviewed:
- writing rules check each requirement's title and statement as it is
  written, mark what they find in the editor, say how to fix it, and give
  each requirement a score
- a glossary defines the project's terms once, marks them where
  requirements use them, and makes those requirements suspect when a
  definition changes
- requirements that say what another already says are found, while writing
  and on a page of likely pairs, and a pair is dismissed, linked or merged
- a Word document comes in as a specification, its headings sections and
  its paragraphs requirements, and a second import of it updates what the
  first made

The epic is #232; this file is #233. The route table in
`backend/internal/httpapi/operations.go` and `operations_m10.go` is the
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
  - 01000 to 01019 for writing rules (#207)
  - 01020 to 01039 for the glossary (#210)
  - 01040 to 01059 for duplicates (#208)
  - 01060 to 01079 for Word import (#209)
- Packages:
  - `internal/quality` (#207): the rules, their word lists, the sentence
    and word reader, the stemmer and term matcher of #210, and the findings
    kept per revision. It reads `internal/document` and never
    `internal/requirement`, which calls it.
  - `internal/glossary` (#210): terms, their revisions and uses.
  - `internal/duplicates` (#208): similar requirements, pairs and what was
    decided about them.
  - `internal/docx` gains a reader beside its writer (#209), and
    `internal/reqifio` builds a Word document into the plan a ReqIF file
    becomes.
- Settings: none in the environment. Each project names its language, its
  rules and word lists, the library glossaries it uses and its similarity
  threshold.
- Extensions: `pg_trgm` (#208), created by the migration as `unaccent` was
  (#20); it is a trusted extension, so the role that migrates may create it.
- Limits:
  - `quality.MaxCustomWords` = 200 per rule and list,
    `quality.MaxWordLength` = 60.
  - `quality.MaxReasonChars` = 500: the reason a finding is accepted.
  - `quality.MaxMatchChars` = 200: the words a finding quotes.
  - `quality.RecheckBatch` = 200: requirements one transaction of a recheck
    takes.
  - `glossary.MaxTerms` = 5000 per project, `glossary.MaxNameLength` = 120.
  - `glossary.MaxSynonyms` and `glossary.MaxAvoid` = 20 per term.
  - `glossary.MaxLibraries` = 20: library glossaries one project uses.
  - `glossary.PreviewChars` = 300: the definition a mark shows.
  - `duplicates.MinThreshold` = 0.30 and `duplicates.MaxThreshold` = 0.95,
    `duplicates.DefaultThreshold` = 0.60.
  - `duplicates.MaxSimilar` = 10: similar requirements listed for one.
  - `duplicates.Neighbours` = 20: the pairs each requirement keeps.
  - `duplicates.MaxTextChars` = 2000: how much of the title and statement
    is compared.
  - A Word upload takes `reqifio.MaxImportMB` = 64, unpacked at most
    `reqifio.MaxUnpackedMB`, and at most `reqifio.MaxObjects` requirements.
  - `reqifio.MaxPreviewNodes` = 2000: the tree a preview shows.
- **Decided** (`docs/decisions.md`, 2026-10-18):
  - the language is the project's, English or German
  - the rules run in Go alone; the editor asks the server as one types
  - findings are stored raw with each revision and judged when read
  - the minimum score is a validator, not a condition
  - a term named on purpose is a mark in the document
  - similar pairs are stored, kept by the database as revisions are written
  - a Word document is imported through the ReqIF pipeline

## The project's language

- `project.settings` gains `language`: `en` or `de`, `en` until an
  administrator sets it, through `PATCH /projects/{projectKey}` as the other
  settings. Anything else is 422 on `language`, "Choose English or German
  as the language of the project's requirements."
- It picks the word lists of the writing rules and the inflections the
  glossary matches. The person's own language picks the words of the
  interface as before; a finding's text in the API and over MCP is
  English.
- Saving it is audited as `project.language_set` and checks the project's
  requirements again (#207, Rechecks).

## Positions

A finding of the rules and a term a requirement uses say where they are as
a `Span`:
- `field`: `title` or `statement`.
- `block`: the text block, counted in document order from 0. The title is
  block 0 of its field. In the statement every paragraph, heading and code
  block is a block, empty ones and those inside lists, quotes and table
  cells included, as the editor counts its text blocks.
- `from` and `to`: offsets within the block as the editor counts them:
  text in UTF-16 code units, and a mention, a line break or an attached
  file as one each.

`document.TextBlocks` produces the blocks in Go, and a fixture it writes is
read by a Go test and a web test alike, so both count the same.

## #207 Writing rules

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/quality-check` | edit | `{check}`; 422 on `statement` |
| `GET /requirements/{reqKey}/quality` | view | `{quality}` |
| `POST /requirements/{reqKey}/quality/accepted` | edit | `{quality}`; 409 `stale_revision`; 422 |
| `DELETE /requirements/{reqKey}/quality/accepted/{findingKey}` | edit | `{quality}`; 409 `stale_revision` |
| `GET /projects/{projectKey}/quality` | view | `{quality}`; 400; 422 `bad_query` |
| `GET /projects/{projectKey}/quality-settings` | view | `{settings}` |
| `PUT /projects/{projectKey}/quality-settings` | administer | `{settings}`; 403; 422 |
| `GET /quality-rules` | signed in | `{rules}` |

### The rules

They read the title and the statement, never the rationale or attributes.
Each finds words or the shape of a sentence, in the project's language:

| Rule | Finds | Severity |
|---|---|---|
| `vague` | words that mean what the reader wants, such as user-friendly, fast, appropriate | warning |
| `unbounded` | words without an end, such as all, any, etc., and so on | warning |
| `escape` | escape clauses, such as if possible, as far as practicable | error |
| `placeholder` | text left open: TBD, TBC, `[...]`, `<...>` | error |
| `and_or` | and/or, und/oder | warning |
| `multiple_shall` | a second shall or must in one sentence, or in a title | warning |
| `negative` | what shall not be, such as shall not, never, darf nicht | hint |
| `passive` | the passive voice without who does it: shall be encrypted, but not shall be encrypted by the gateway | warning |
| `pronoun` | a pronoun without the thing it stands for before it, such as It shall at the start | warning |
| `ears` | a sentence with a shall that fits no shape of the EARS patterns, or no shall at all | hint |
| `avoid_term` | a word the glossary says not to use (#210) | warning |

- **Word lists** per language live in the package. A project adds words to
  `vague`, `unbounded`, `escape` and `placeholder`, and excepts built-in
  words it does not want found, at most `MaxCustomWords` each.
- **EARS shapes.** Each sentence with a shall is `ubiquitous` (The system
  shall), `event` (When, Wenn, Sobald), `state` (While, Während, Solange),
  `unwanted` (If ... then, Falls ... dann), `optional` (Where, Sofern) or
  `complex`. `ears` finds an opener of none of them, an If without then, a
  trigger at the end ("shall stop when ..."), a German sentence whose verb
  does not follow the clause, and a statement without a shall. The check
  answers each sentence's shape, for the editor to show.
- **Without a network or a language model.** The rules are plain Go
  functions over the plain text of `internal/document`.

### Findings and the score

- **A finding** is a rule, its severity, a `Span`, the words it matched,
  and English sentences saying what is wrong and how to fix it. Its `key`
  is the rule, the field, the words and which occurrence of them it is, so
  the same finding keeps its key across revisions while the words stay.
- **What is kept per revision.** Every revision a requirement gets, by any
  writer (editing, moving, copying, restoring, reuse, templates, every
  import), has its findings of every rule written in the same transaction,
  with the words the project's lists held at the time. Switched-off rules
  are kept too.
- **What is judged when read.** Whether a rule is on, its severity, the
  types it applies to and the findings accepted are applied when the score
  is read, in the database. Switching a rule or changing its severity
  takes effect at once, and the query language, the workflow and the pages
  agree.
- **The score** is 100 less, for each rule, the weights of its active
  findings, at most twice its weight per rule: an error weighs 20, a
  warning 7, a hint 2. It is 0 at least. A section has none, and a
  requirement not checked yet has none.
- **Rechecks.** A changed word list, language or glossary avoid words
  (#210) changes what the rules find. The worker checks the current
  revision of each of the project's requirements again,
  `quality.RecheckBatch` at a time; until it reaches one, its quality says
  `stale`. The migration asks for a recheck of every project, which scores
  the requirements there are.

### The check as one types

- **`POST /projects/{projectKey}/quality-check`** takes a draft:
  `{title, statement, typeId?, requirementKey?}`, the statement a document
  of the shared editor, or `statementMarkdown` in its place, as an
  assistant writes it. It answers what saving it would find: `score`,
  `findings`, `sentences` with their shapes, and `terms`, the glossary
  terms it uses (#210). Nothing is written. `requirementKey` names the
  requirement being revised, so findings accepted on it come with the
  answer.
- A statement that is not a document of the editor is 422 on `statement`,
  as when it is saved.
- The editor sends the draft once typing pauses
  (`QUALITY_CHECK_DEBOUNCE_MS`, 500), drops an answer to a draft it has
  moved past, and marks each finding at its `Span`.

### Accepting a finding

- **`POST /requirements/{reqKey}/quality/accepted`**
  `{revision, key, reason}` accepts a finding of the current revision: it
  stays shown, marked accepted, and no longer counts. The reason is 1 to
  `MaxReasonChars` characters.
- A revision that is not the current one is 409 `stale_revision`, "The
  requirement changed since you read it. Read it again, then accept the
  finding." A key the revision does not have is 422 on `key`.
- **It stays with the revision.** A new revision whose findings hold the
  same key carries the acceptance forward, saying which revision it was
  first given for; a finding whose words changed is new and counts again.
- `DELETE .../accepted/{findingKey}` takes it back, on the current
  revision.

### Settings

- **`PUT /projects/{projectKey}/quality-settings`** sends every rule and
  every word list at once: per rule `enabled`, `severity` and `typeIds`,
  the types it applies to, every type when empty; per word rule `add` and
  `except`. An unknown rule or type is 422 on it; a word longer than
  `MaxWordLength` or a list longer than `MaxCustomWords` is 422 on it.
  Saving is audited as `quality_settings.saved`.
- `GET /quality-rules` lists every rule with its default severity, whether
  it starts on, the fields it reads, whether it takes words, and an English
  label and description. The web shows its own words for them.

### Where the score shows

- `Requirement` gains `quality`: `{score, active, accepted, stale}`.
  `TreeNode` and the grid's rows gain `quality`, the score alone, and the
  grid sorts by it (`sort=quality`).
- **`GET /projects/{projectKey}/quality`** answers how many requirements
  score in each band of 20, how many are not checked, the average, how
  many requirements break each rule, and the requirements lowest first, a window at
  a time, narrowed as the grid is.
- The project gains a quality page with the bands, the rules found most and
  the list; the specification and the grid show each score.

### The query language

| Field | Kind | Meaning |
|---|---|---|
| `quality` | number | the score; empty for sections and requirements not checked |
| `qualityFinding` | set of rules | the rules with an active finding, such as `qualityFinding = passive`; `qualityFinding IS EMPTY` |

The query help and both languages of the interface follow.

### The workflow

- A transition rule `validator.quality_min` with an option `minimum`, a
  whole number from 0 to 100, refuses the transition while the
  requirement scores below it: "CHK-R4 scores 55; this transition asks for
  70. Fix or accept its findings, then take it again." A requirement not
  checked yet is refused with "CHK-R4 is not checked yet. Wait a moment,
  then take the transition again."
- **A validator, not a condition**, though #207 says condition: a
  condition hides a transition without saying why, and the author needs to
  know what to fix. It sits beside the trace rules of #206.
- Rules gain an option kind `number`, with a least and a greatest value.

### Guards

- Findings and the score are written only for the current revision of a
  requirement the writer may edit, in the transaction that wrote the
  revision; nobody updates them. A viewer cannot raise a score.
- An acceptance names a finding the revision has and the person giving it,
  and holds for the current revision only.
- Rules and word lists are written by the project's administrators, in an
  open project.

All of it is held by the database and proven straight through SQL as
`exciter_app`.

### Refusals

| Status | Code | When |
|---|---|---|
| 403 | `forbidden` | settings by somebody who does not administer the project |
| 409 | `stale_revision` | accepting or taking back on a revision that is not current |
| 409 | `project_archived` | any write in an archived project |
| 422 | `validation_failed` | a draft that is not a document, an unknown rule, key or type, a word or list too long, a reason empty or too long |

### MCP

- `check_requirement_text` reads `POST /projects/{projectKey}/quality-check`,
  so an assistant checks what it is about to write.
- `get_quality_findings` reads `GET /requirements/{reqKey}/quality`.

### UI

- The statement editor marks each finding, underlined by severity, and
  says the rule and the fix on hover or focus. A list below the editor
  holds the same findings for a keyboard and a screen reader, each with
  "Show in text" and, on a saved requirement, "Accept" with a reason.
- Reading a requirement, "Show findings" marks them in its text.
- The project's settings gain a quality tab: the rules, their severities
  and types, and the word lists; the language is chosen with the project's
  details.

### As built (#207)

- **Migration**: `01000_quality.sql` adds:
  - the CHECK on `project.settings->>'language'`
  - the tables `quality_rule`, `quality_rule_setting` (word lists as its
    `add_words` and `except_words`), `project_quality`,
    `requirement_quality`, `quality_finding` and `quality_acceptance`
  - the functions `quality_rule_state`, `requirement_quality_of`,
    `quality_weight` and `quality_words_fit`
  - one `quality.recheck` event per project, which scores what is there
- **Packages**: the rules engine, `Record` and the service are in
  `internal/quality`. It finds a requirement by key through a `Locator`
  the requirement package gives it (`requirement.LocateForQuality`), since
  the requirement package calls it. The project's quality page is
  `search.Quality`, which narrows as the grid does.
- **Where it runs**: `quality.Record` follows every insert of a revision in
  `internal/requirement`: create, save, move, copy, restore, reuse and the
  template's sections. A section is not checked.
- **What the database judges**: `requirement_quality_of` answers the score,
  the counts, the rules that count and whether it is stale; the
  requirement, the tree, the grid, the query language and the workflow
  read it. Go judges a revision the same way, and a test holds the two to
  each other.
- **Rechecks** run as the admin role, which alone writes `project_quality`.
  An event for one carries the actor, as every event of the outbox does.
- **Limits**: a revision keeps at most `quality.MaxFindings` = 500
  findings. A finding's key holds spaces and slashes; a client encodes it
  in the path.
- **Positions**: `document.TextBlocks` writes `api/document-textblocks.json`
  in `make openapi`, which the web test reads.
- **Word lists**: German "kann" is not an escape word, since it states a
  capability; "may" also finds the month, which a project excepts if it
  needs to.

## #210 The glossary

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/terms` | view | `{terms, total, limit, offset}` |
| `POST /projects/{projectKey}/terms` | edit | 201 `{term}`; 409 `term_exists`; 422 |
| `GET /terms/{termID}` | view of its project | `{term}` |
| `PATCH /terms/{termID}` | edit of its project | `{term}`; 409 `stale_revision`, `term_exists`; 422 |
| `DELETE /terms/{termID}` | administer of its project | 204 |
| `GET /terms/{termID}/revisions` | view of its project | `{revisions}` |
| `GET /terms/{termID}/revisions/{revisionNumber}` | view of its project | `{revision}` |
| `GET /terms/{termID}/uses` | view of its project | `{uses, total, limit, offset}` |
| `GET /requirements/{reqKey}/terms` | view | `{terms}` |
| `POST /requirements/{reqKey}/terms/{termID}/clear` | edit | `{term}`; 409 `stale_revision`; 422 |
| `GET /projects/{projectKey}/glossaries` | view | `{glossaries}` |
| `PUT /projects/{projectKey}/glossaries` | administer | `{glossaries}`; 403; 422 |

### Terms

- **A term** belongs to one project: a name of at most `MaxNameLength`
  characters, a definition (a document of the shared editor), synonyms and
  words not to use instead, at most `MaxSynonyms` and `MaxAvoid`, and a
  status: `proposed`, `approved` or `deprecated`.
- **One word, one term.** No two terms of a project share a name or a
  synonym, regardless of case: 409 `term_exists`, "<name> already names a
  term of this glossary. Change that term, or choose another name."
- **Terms have revisions**, as requirements do, and are not requirements:
  they never count in coverage, the matrix or a baseline. A change names
  the revision it was read at (`baseRevision`), 409 `stale_revision`
  otherwise. Each term keeps `definitionRevision`, the revision its name or
  definition last changed in.
- **Deleting** a term deletes its revisions; words linked to it read as
  plain words. It is audited as `term.deleted`.
- **Search** finds terms: `GET /search` gains `terms`, the best five of the
  project's own and its libraries' that the caller may read.

### Library glossaries

- **A library project** (#49) may hold a glossary for many. A project's
  administrators name the libraries whose glossaries mark its requirements,
  in order, through `PUT /projects/{projectKey}/glossaries`; the project's
  own terms come first and then each library's, the first winning a word
  two share. Saving is audited as `glossaries.chosen`.
- A project that is not a library, the project itself, or one the
  administrator may not read is 422 on `libraries`, "Choose library
  projects you may read." More than `MaxLibraries` is 422.
- **A reader sees a library's terms only with view on the library**: its
  marks, uses and definitions stay away from anybody else.

### Where a requirement uses a term

- **By its words.** The rules' reader matches each term's name and synonyms
  in the title and statement, as words, with their inflections in the
  project's language: plurals, cases and verb endings in English and
  German, an acronym as written. The longest match wins.
- **By an explicit link.** An author marks words and links them to a term,
  for words the matcher would not find. The link is a mark in the
  document, `term` with the term's id, so it moves with the text it is on.
  The editor, Word, Markdown, the print view and ReqIF all know the mark:
  Word links it to the term in the glossary chapter, and the others write
  its words.
- **The uses are kept** for the current revision of each requirement, as
  its findings are, and `GET /terms/{termID}/uses` lists the requirements
  the caller may read.
- **A word not to use** is a finding of `avoid_term` (#207), saying which
  term to use instead. Changing the words of a term checks the requirements
  of its project, and of every project that uses its library, again.

### A changed definition makes a requirement suspect

- **A use is suspect** once the term's `definitionRevision` is past the
  revision the use last saw, as a suspect link is (#36): the requirement
  was written against an older meaning.
- It counts in the requirement's suspect links, so the query language's
  `suspect`, the workflow's trace rules and the grid follow.
- **Its owner confirms it** with
  `POST /requirements/{reqKey}/terms/{termID}/clear`
  `{termRevision, comment}`, naming the term's current revision, 409
  `stale_revision` otherwise. The confirmation is kept as a link's is.
- **A notification** `term_changed` tells the owner of each requirement
  that uses the term, once per term revision, however many requirements.

### The query language

| Field | Kind | Meaning |
|---|---|---|
| `term` | text | a term the requirement uses, by its name or a synonym, such as `term = "brake pedal"`; `term IS EMPTY` |

### Exports

- **A project's export** in Word, PDF, Markdown and ReqIF carries its
  glossary: the terms that are not deprecated, of the project and of its
  libraries the reader may read, by name. A section's export carries the
  terms its requirements use. A baseline's export carries none yet.
- Word adds a Glossary chapter after the requirements, PDF the same,
  Markdown a `## Glossary` part, ReqIF a spec type `exciter-glossary-term`
  and a second specification, which a ReqIF import leaves out with a loss.

### Guards

- A term's revisions are only ever added to.
- A use is written with the revision it was seen at, for the current
  revision of a requirement the writer may edit; only a confirmation moves
  what it has seen, and only to the term's current revision.
- A project names only library projects, other than itself.

### Refusals

| Status | Code | When |
|---|---|---|
| 403 | `forbidden` | deleting a term or choosing libraries without administer |
| 409 | `term_exists` | a name or synonym another term of the glossary has |
| 409 | `stale_revision` | a change or confirmation against a revision that is not current |
| 422 | `validation_failed` | a name empty or too long, too many synonyms or words, a definition that is not a document, a library that cannot be chosen |

### MCP

`list_terms` reads `GET /projects/{projectKey}/terms`. The check of #207
answers the terms a draft uses.

### UI

- A glossary page lists the terms; a term's page shows its definition, its
  revisions and their differences, and the requirements that use it.
- The editor marks each term a requirement uses and shows its definition
  on hover, or when the caret is in it. "Link to term" links words to a
  term on purpose. Reading a requirement, a term is a button that shows
  its definition.
- The requirement page gains a terms panel, a suspect use marked, with
  "Confirm".
- The project's settings gain the library glossaries it uses.

### As built (#210)

- **Migration**: `01020_glossary.sql` adds:
  - the tables `glossary_term`, `glossary_term_revision`, `project_glossary`,
    `term_use` and `term_use_clearance`
  - the functions `glossary_terms_of`, `glossary_users` and `term_use_clear`,
    and the triggers that number a term's revisions and write them
  - `quality_finding.use_instead`, the term an `avoid_term` finding names
  - `notification.term_id` and the kind `term_changed`
- **Revisions are the database's**: a change of a term is the next revision,
  numbered by a trigger, which also writes the revision as the definer; the
  app has no grant on the revisions at all. A change that changes nothing is
  no revision.
- **A confirmation** is kept in `term_use_clearance`, a table of its own,
  rather than among the clearances of links, which name a link.
- **Uses** are written with the findings, by `quality.Record`, as the terms
  the writer may read; a library the writer may not read marks nothing for
  them, and a use they may not see is neither written nor removed by them.
  The matcher stems in the project's language without a library, matches
  multi-word terms within a sentence, and lets a project's own term win
  over a library's.
- **Words to check again**: a term made, changed in its words or deleted, and
  a project's glossaries chosen, have the worker check every project the
  glossary marks again (`glossary_users`).
- **A notification** of a changed definition names one requirement of each
  owner, and the term as its excerpt.

## #208 Duplicates

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/similar` | edit | `{similar, threshold}`; 422 |
| `GET /requirements/{reqKey}/similar` | view | `{similar, threshold}` |
| `GET /projects/{projectKey}/duplicates` | view | `{pairs, next, threshold}`; 400; 422 `bad_query` |
| `POST /projects/{projectKey}/duplicates/dismiss` | edit | 201 `{decision}`; 409 `stale_revision`; 422 |
| `POST /projects/{projectKey}/duplicates/merge` | edit on both | `{merge}`; 409 `stale_revision`; 422 |
| `POST /projects/{projectKey}/duplicates/link` | edit | 201 `{decision}`; 409 `stale_revision`; 422 |
| `GET /projects/{projectKey}/duplicate-settings` | view | `{settings}` |
| `PUT /projects/{projectKey}/duplicate-settings` | administer | `{settings}`; 403; 422 on `threshold` |

### Similarity

- **Two requirements are alike** by the trigrams of their title and the
  plain text of their statement, at most `MaxTextChars`, as `pg_trgm`
  judges them: a score from 0 to 1. Case does not matter.
- **The threshold** is the project's, `DefaultThreshold` until an
  administrator sets one from `MinThreshold` to `MaxThreshold`. Saving it
  is audited as `duplicate_settings.saved`.
- **While writing**, `POST /projects/{projectKey}/similar` takes the draft
  and answers at most `MaxSimilar` requirements above the threshold, most
  alike first: of the project, and of library projects the caller may read,
  each marked `library`. `excludeKey` leaves out the requirement being
  revised. The editor asks with the check of #207, in the same pause.
- **`GET /requirements/{reqKey}/similar`** answers the same for a saved
  requirement, leaving out pairs that stay decided.
- Sections, and requirements in the trash, are never compared.

### Pairs

- **The database keeps candidate pairs.** Whenever a requirement gets a
  revision, its `Neighbours` most alike requirements of its project, at
  `StoredThreshold` or above, are kept as pairs with their score. Nobody
  writes them but the database.
- **`GET /projects/{projectKey}/duplicates`** answers the pairs above the
  threshold, most alike first, a page at a time with `next`, narrowed by
  `type`, `section` or `query`, which either side may match. Both sides are
  ones the caller may read. A pair decided about is left out while the
  decision holds.

### Deciding about a pair

Each decision names both sides at the revisions they were read at, 409
`stale_revision` when either moved on.
- **Dismiss** `{a, b, reason}`: the two do not say the same thing. The pair
  stays away until either side is revised; then it is judged again.
- **Link** `{source, target, linkTypeKey}`: the two are related, by one of
  the project's link types, under the link rules of #36. The pair stays
  away while the link lasts.
- **Merge** `{keep, trash, comment}`: the side to trash goes to the trash,
  and each of its links is made again on the side kept, in one
  transaction. A link that would join the kept side to itself, that it
  already has, or whose type no longer allows it, is skipped and said why.
  The trashed requirement keeps its own links, so restoring it brings them
  back.
- Every decision is kept, who and when, and only ever added to.

### Guards

- Pairs are written by the database alone.
- A decision names two requirements of its project; a merge's trashed side
  is in the trash and its kept side is not.

### Refusals

| Status | Code | When |
|---|---|---|
| 403 | `forbidden` | the threshold without administer |
| 409 | `stale_revision` | a side was revised since it was read |
| 422 | `validation_failed` | a side that is not a requirement of the project, the same requirement twice, a reason too long, an unknown link type, a threshold out of range |

### MCP

`get_similar_requirements` reads `GET /requirements/{reqKey}/similar`. No
tool decides.

### UI

- Below the statement editor, a hint lists the requirements the draft is
  most like; a library's offer "Reuse instead" (#49).
- A project page lists the likely duplicates, side by side with their
  differences, each with Dismiss, Link and Merge.
- The threshold is set in the project's quality settings.

### As built (#208)

- **Migration**: `01040_similarity.sql` creates `pg_trgm` and adds:
  - `requirement.similarity_text`, the compared words, with a trigram GIN
    index over requirements
  - the tables `requirement_similarity`, `duplicate_decision` and
    `duplicate_settings`
  - the functions `requirement_similarity_refresh`, run after every
    revision by a trigger, `duplicate_pair_settled` and the guard
    `duplicate_decision_holds`
  - one refresh of every requirement there is
- **The index is GIN**, filtered by the `%` operator at the threshold, rather
  than GiST nearest-neighbour ordering: a project narrows the candidates
  first, which a nearest-neighbour scan of the whole installation would not.
- **A decision holds** while a dismissal's sides keep their revisions or a
  link lasts; any decision that holds keeps a pair away. A merge needs none,
  since its trashed side is left out anyway.
- **Merging** moves each live link of the side that goes in a savepoint of
  its own, so a link the database refuses on the side kept is skipped with
  the reason; then that side goes to the trash, and the decision is kept,
  in one transaction. Linking uses the link rules of #36 in the same way.
- **The pairs page** pages with `next`, an opaque cursor.
- **The threshold** is a table of its own rather than a project setting, so
  its writes are guarded and audited like the other settings of M10.

## #209 Word import

| Operation | Needs | Answers |
|---|---|---|
| `POST /projects/{projectKey}/word-imports` | administer | 201 `{import}`; 403; 409; 413 `too_large`; 422 |
| `GET /reqif-imports/{importID}` | as for ReqIF | `{import}` |
| `PUT /reqif-imports/{importID}/choices` | as for ReqIF | `{import}`; 409 |
| `POST /reqif-imports/{importID}/start` | as for ReqIF | 202 `{import}`; 409 `import_problems` |
| `DELETE /reqif-imports/{importID}` | as for ReqIF | 204 |

### The upload

- **A `.docx`**, sent as the multipart part `file`, with `parentKey`, the
  section to import below; the project's top when empty. A `parentKey`
  that is not a section of the project is 422 on it. A file that is not a
  Word document is 422 on `file`, "<name> is not a Word document. Save it
  as .docx and upload it again."
- **Nothing is written yet.** The answer is an import of format `docx`,
  previewed as a ReqIF import is, and it goes on through the ReqIF
  import's operations: choices, start, progress, discard.
- `Preview` gains `tree`, for Word and ReqIF alike: each part of the
  document in order, its depth, what it becomes (`section`,
  `requirement`, `informative`, `skipped`), its title, where it is in the
  document, and, for one an earlier import made, which requirement and how
  it was found. At most `MaxPreviewNodes`, the counts kept whole.

### What becomes what

- **Headings become sections**, by level, under the target. A heading is a
  paragraph of a heading style or with an outline level.
- **Below a heading**, paragraphs, lists, tables and pictures become
  requirements, as chosen (`word.unit`):
  - `paragraph`: one requirement per paragraph, a list joining the
    paragraph before it when that ends with a colon, a picture its caption
  - `heading`: one requirement per heading without headings below it,
    holding everything below it
- **What is a requirement** and what is informative text that joins the
  statement of the section above it is decided by the paragraph styles
  (`word.styles`), a pattern its number or first words match
  (`word.pattern`, such as `REQ-\d+`) and keywords (`word.keywords`,
  `shall, must` or `muss, soll` by the project's language). Any one
  matching makes a requirement; with none set, every paragraph is one.
- **A table** is one requirement or one per row (`word.table.<n>`). Per
  row, its header row is a type whose columns the schema step maps to the
  title, the statement, the rationale and attributes, as a ReqIF
  attribute is mapped.
- **Text before the first heading** becomes a section named after the
  document's title, or is skipped (`word.front`).
- **Rich text** keeps bold, italics, strike, lists, tables with merged
  cells, links and pictures, as the editor holds them. What it cannot keep
  is a loss: underlining, colours, deleted text of tracked changes,
  comments, footnotes, equations, text boxes and nested tables.
  Insertions of tracked changes are kept.

### A second import

- **Importing the same document into the same place** updates what the
  first import made as new revisions, and leaves unchanged what did not
  change. An import is tied to the document and its target section; what
  each part made is tied to the first of:
  1. the key in Exciter's own Word export, which also offers the layout
     that export writes
  2. the number the pattern matched
  3. the identifier Word keeps for a paragraph
  4. the headings above it and its place among them
- The preview says which tie each part found. A part the document no
  longer holds is left as it is.

### Guards

- The import is the ReqIF import's, with its guards: made by the project's
  administrators, its file fixed, its state only moving forward.
- Its target section is a section of the same project, fixed once made.

### MCP

None: an import is a person's.

### UI

The project's import settings gain Word: the upload with the section to
import below; the choices with the document's styles and how often each
is used, the pattern, the keywords and each table's; the schema step for
tables by row; the preview tree; the progress and the losses, as ReqIF
shows them.

### As built (#209)

- **Migration**: `01060_word_import.sql` lets `reqif_import.format` be
  `docx`, adds `parent_id`, the section an import goes below, held to a
  section of the same project, and lets `total` follow the choices until the
  import starts.
- **Packages**: the reader is `docx.Read`, beside the writer; the bounded
  reading of an archive moved from `reqifio` into `internal/ziplimit`,
  which both use. `reqifio` keeps the document as read and builds what it
  becomes from the choices at every analysis, so the preview, the schema
  step, the run, the ties and the progress are the ReqIF import's.
- **Choices**: an empty `word.styles` or `word.pattern` sets none, and an
  empty `word.keywords` clears the keywords; with all three empty every
  paragraph is a requirement, and with none given the keywords of the
  project's language apply. A choice that cannot hold, such as a pattern
  that is no regular expression, is 422 on its key from the choices.
- **What becomes what**:
  - A first paragraph in Word's "Title" style names the document rather
    than heading it.
  - Numbering of letters, such as `REQ-%1`, stays with its paragraph and
    gives the title and the number identity; numbering of numbers alone is
    a list.
  - A table imported whole is a requirement, or joins a paragraph before it
    that ends with a colon. Informative text is the statement of the section
    above it.
  - Re-reading Exciter's own export leaves out its cover, contents,
    glossary, notes, properties tables and link lists, and its "Rationale"
    label fills the rationale.
- **Identities**: a requirement keeps one identity of a Word import, which
  holds every way to find the part again
  (`word:<document>@<target>:key:..|number:..|paragraph:..|path:..`); a
  second import tries each in turn, and never takes over a part a ReqIF
  import made. A long path is hashed to fit. A second import moves the parts
  back into the document's order.
- **Known gaps**: a part with a picture counts as changed on every second
  import, since its picture is attached again; a mention read back from an
  export is plain text.

## What is left for later

- A query language over the terms themselves.
- Compound words in German, such as a term Ventil found in Bremsventil.
- A glossary in a baseline and its exports, which needs terms recorded in
  baselines.
- Terms imported from ReqIF or Word.
- Rules over the rationale and attributes, and rules that need a language
  model.
- Pairs across projects on the duplicates page; a library's requirements
  show only while writing and over MCP.
- Undoing a dismissal; a revision undoes it.
- Trashing what a second Word import no longer holds.
- Word's comments, footnotes and equations, which are losses.
