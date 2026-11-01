# Functional requirements / Search

How requirements are found and listed.

### FR-SRC-1 Search requirements by their words
type: Functional
priority: must
verification: Test
component: Search
refines: SN-7
issues: EXC-20
guide: search

When a person searches for words, Exciter shall answer the requirements they may view whose title, statement, rationale or text values hold them, ignoring case and accents, and shall order the title's matches first.

Rationale: A large specification is read by looking things up.

### FR-SRC-2 Narrow requirements with a query
type: Functional
priority: should
verification: Test
component: Search
refines: SN-7
relates: FR-SRC-1
issues: EXC-47
guide: search

When a person writes a query such as `status = Approved ORDER BY key`, Exciter shall list the project's requirements it selects, and when the query does not read, Exciter shall say at which character.

Rationale: Questions about fields and links are asked by many people; a query lets them ask once and reuse the answer.
