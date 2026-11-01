# Functional requirements / Documents

How the specification is written and read as one document, and how it leaves Exciter.

### FR-DOC-1 Write the specification in place
type: Functional
priority: should
verification: Test
component: Documents
refines: SN-3
issues: EXC-166, EXC-168, EXC-170
guide: documents

When a person who may edit opens the specification of a project or a section, Exciter shall let them edit a requirement where it is read, change the status of one or many requirements, and mark what changed since the previous revision, the last approval or a baseline.

Rationale: Writers read a specification as a document; sending them to a form per requirement breaks the reading.

### FR-DOC-2 Export a specification to Word and PDF
type: Functional
priority: must
verification: Test
component: Documents
refines: SN-8
relates: FR-DOC-1
issues: EXC-45
guide: documents

When a person exports a project, a section or a baseline, Exciter shall produce one document with a cover, contents, and for each requirement its text, a table of its fields and its links, as Word or as PDF.

Rationale: Customers and suppliers receive documents, not accounts.

### FR-DOC-3 Export through a Word template or as a static site
type: Functional
priority: could
verification: Test
component: Documents
refines: SN-8
relates: FR-DOC-2
issues: EXC-214
guide: documents

When a project administrator has stored a Word template, Exciter shall export a specification through it, and when a person asks for a static site, Exciter shall produce pages that open offline.

Rationale: An organisation's documents have its own look.
