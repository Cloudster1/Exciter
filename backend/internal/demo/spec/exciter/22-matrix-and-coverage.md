# Functional requirements / Matrix and coverage

How a team sees what is decomposed, implemented and verified.

### FR-MAT-1 Count coverage by rule
type: Functional
priority: must
verification: Test
component: Coverage
refines: SN-2
issues: EXC-38
guide: matrix-and-coverage

When an administrator has set coverage rules for a requirement type, Exciter shall show for each requirement of that type whether it is decomposed, implemented and verified as none, partial or full, counting only links a reader may see and no suspect link as full.

Rationale: A team must see at a glance which requirements nothing verifies.

### FR-MAT-2 Show the trace matrix with its gaps
type: Functional
priority: should
verification: Test
component: Coverage
refines: SN-2
relates: FR-MAT-1
issues: EXC-38, EXC-147, EXC-148
guide: matrix-and-coverage

When a person opens the matrix, Exciter shall let them pick its rows, columns, link types and filters, and shall mark the cells that have no link where one is expected.

Rationale: A gap is only found if the matrix shows what is missing.

### FR-MAT-3 Edit links from a matrix cell
type: Functional
priority: could
verification: Test
component: Coverage
refines: SN-1
relates: FR-MAT-2, FR-TRC-1
issues: EXC-149
guide: matrix-and-coverage

When a person who may edit selects a cell of the matrix, Exciter shall make or take off the link it stands for, as the requirement's page does.

Rationale: Closing a gap should not need leaving the matrix.

### FR-MAT-4 Follow a trace model in a chain report
type: Functional
priority: should
verification: Test
component: Coverage
refines: SN-2
relates: FR-MAT-1
issues: EXC-151
guide: matrix-and-coverage

When an administrator has named the levels of a project's trace model, Exciter shall report from each root requirement the chain through those levels, and shall mark the chains that break.

Rationale: A project traces from needs to tests in steps it chooses, and wants the broken chains named.
