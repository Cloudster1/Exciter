# Non-functional requirements

Criteria are measured with the k6 load scripts in the standard profile (40 readers, 8 editors and 24 people in shared drafts, three minutes) against a space of 2,000 pages, on a reference machine with one api process, one worker and a database primary with a replica.

## Performance

### NFR-PERF-1 Reading stays fast under a team's load
type: Non-functional
priority: must
verification: Test
component: Performance
refines: SN-10
issues: STA-368, STA-371
guide: page-tree

Under the standard load profile, Stator shall answer the reads of a space, of the children of a page and of a page within 300 ms at the 95th percentile, and the whole page tree of a space of 2,000 pages within 1 s at the 95th percentile.

Rationale: A wiki that is slow to open is not opened.

### NFR-PERF-2 Search stays fast with rights checked
type: Non-functional
priority: must
verification: Test
component: Performance
refines: SN-10
issues: STA-370
guide: search

Under the standard load profile, Stator shall answer a search, with the rights of the searcher checked on what matches, within 800 ms at the 95th percentile.

Rationale: Search was once slow because rights were checked page by page; the criterion keeps it from coming back.

### NFR-PERF-3 Writing together is immediate
type: Non-functional
priority: should
verification: Test
component: Performance
refines: SN-3
issues: STA-65, STA-368
guide: editing-together

Under the standard load profile, Stator shall deliver an update from one person in a shared draft to the others within 300 ms at the 95th percentile, open a shared draft within 500 ms at the 95th percentile, and answer a draft save, a publish or a comment within 1 s at the 95th percentile.

Rationale: Typing together only works when what one person types appears for the others at once.

### NFR-REL-1 Requests under load rarely fail
type: Non-functional
priority: should
verification: Test
component: Reliability
refines: SN-10
issues: STA-368, STA-381
guide: working-in-the-suite

Under the standard load profile, fewer than 1 % of the requests of each scenario shall fail, a stale version answered with 409 not counted as a failure.

Rationale: A busy team must not meet errors as a matter of course.

## Usability, safety and operation

### NFR-ACC-1 Text and controls are readable
type: Non-functional
priority: must
verification: Test
component: Web
refines: SN-10
issues: STA-8, STA-133, STA-135, STA-189
guide: working-in-the-suite

Every page of the web application shall pass the automated accessibility checks of axe for WCAG level AA, in the light and dark themes, and subtle text, code colours and the danger button shall reach a contrast of at least 4.5 to 1.

Rationale: People with low vision read the same wiki as everybody else.

### NFR-I18N-1 Use English and German
type: Non-functional
priority: should
verification: Demonstration
component: Web
refines: SN-10
issues: STA-113
guide: working-in-the-suite

Stator shall show its interface in English or German as the person chooses, and keep the choice for the person's next visits.

Rationale: Teams of the suite write in both languages.

### NFR-SEC-1 Rules hold in the database
type: Non-functional
priority: must
verification: Test
component: Security
refines: SN-10
issues: STA-19, STA-395, STA-401, STA-423
guide: permissions

Stator shall enforce the rules on who may view or change pages, comments, tokens and member roles in the database with row level security, so that a request that reached the database around the service is refused the same way.

Rationale: A mistake in one handler must not open a page to the wrong person.

### NFR-OPS-1 Restore from one archive
type: Non-functional
priority: should
verification: Test
component: Operation
refines: SN-10
issues: STA-108, STA-532
guide: spaces-and-pages

Stator shall take a backup as one archive of the database and the file store with one command, and shall refuse to restore it over a running installation without the explicit flag -yes.

Rationale: A restore that overwrites data by accident is worse than none.
