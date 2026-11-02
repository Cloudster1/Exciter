# Functional requirements / Import and export

How requirements come in from and go out to other tools.

### FR-IMP-1 Import and export CSV
type: Functional
priority: should
verification: Test
component: Import and export
refines: SN-8
issues: EXC-23
guide: import-export

When a person uploads a CSV file, Exciter shall propose a mapping from its headers, show what each row would do, run the import as that person with each row in its own step, and export the grid's query as CSV.

Rationale: Spreadsheets are where most specifications start.

### FR-IMP-2 Exchange a specification as ReqIF
type: Functional
priority: should
verification: Test
component: Import and export
refines: SN-8
issues: EXC-44
guide: import-export

When a person imports a ReqIF file, Exciter shall match types, attributes and link types by the identifier an earlier exchange tied and then by name, make the rest, write the requirements and relations, and write the same identifiers back on export.

Rationale: ReqIF is the format customers and suppliers exchange requirements in; a second exchange must update, not duplicate.

### FR-IMP-3 Import a Word document as a specification
type: Functional
priority: could
verification: Test
component: Import and export
refines: SN-8
relates: FR-IMP-2
issues: EXC-209
guide: import-export

When a person imports a Word document, Exciter shall make its headings sections and its paragraphs requirements, and when the same document is imported again, shall update what the first import made.

Rationale: Many specifications exist only as Word files.
