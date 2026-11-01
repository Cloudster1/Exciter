# Functional requirements / Quality

How authors are helped to write clear, consistent requirements.

### FR-QLT-1 Check requirements against writing rules
type: Functional
priority: should
verification: Test
component: Quality
refines: SN-11
issues: EXC-207
guide: quality

When a person writes a title or statement, Exciter shall check it against the project's writing rules, mark what they find in the editor with how to fix it, and give the requirement a score.

Rationale: A vague word is cheapest to remove when it is typed.

### FR-QLT-2 Define terms in a glossary
type: Functional
priority: should
verification: Test
component: Quality
refines: SN-11
relates: FR-TRC-2
issues: EXC-210
guide: quality

When a project administrator defines a term, Exciter shall mark it where requirements use it, and when the definition changes, make the requirements that use it suspect.

Rationale: A term changed in one place changes what many requirements mean.

### FR-QLT-3 Find requirements that say the same
type: Functional
priority: should
verification: Test
component: Quality
refines: SN-11
issues: EXC-208, EXC-49
guide: quality

When a person writes a requirement, Exciter shall point at requirements that say much the same, and on a page of likely pairs let a person dismiss, link or merge a pair; a library's requirement copied into another project shall follow its source.

Rationale: Saying a thing twice makes the copies drift apart.
