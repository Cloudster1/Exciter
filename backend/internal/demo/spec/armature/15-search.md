# Functional requirements / Search and reports

How work is found and handed on.

### FR-SRC-1 Find issues by field, text and custom field
type: Functional
priority: must
verification: Test
component: Search
refines: SN-6
issues: ARM-83, ARM-87, ARM-293
guide: search

When a person searches with NQL, Armature shall answer the issues that match, including a negation that keeps the issues with nothing in the field, a custom field compared with a number or a date, and a text match on the start of each word of a description.

Rationale: People search for what they remember, which is a word, a number or a date, and a search that answers an error for a valid query is not trusted again.

### FR-SRC-2 Page through every result
type: Functional
priority: should
verification: Test
component: Search
refines: SN-6
issues: ARM-90
guide: search

When a search or a project's issue list holds more than 100 issues, Armature shall let the person page to every result.

Rationale: A list that stops at 100 hides the oldest work, which is the work nobody remembers.

### FR-SRC-3 Export results to CSV up to 5,000 issues
type: Functional
priority: should
verification: Test
component: Search
refines: SN-6
issues: ARM-80, ARM-121
guide: search

When a person exports the results of a search, Armature shall write a CSV file with up to 5,000 issues and shall say so when it cut the list.

Rationale: A report goes to people who do not use Armature; a file that quietly stops at 50 issues is a wrong report.

### FR-SRC-4 Share a dashboard that is computed on read
type: Functional
priority: could
verification: Test
component: Search
refines: SN-6
issues: ARM-57, ARM-58
guide: search

When a person opens a dashboard, also by a share link, Armature shall compute every number when the page is read, and a shared dashboard's widgets shall work for visitors.

Rationale: A number stored on Monday is wrong on Tuesday; a widget that fails for visitors makes the share link useless.
