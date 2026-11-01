# Functional requirements / Requirements

How requirements are written, ordered and kept.

### FR-REQ-1 Create and edit a requirement
type: Functional
priority: must
verification: Test
component: Requirements
refines: SN-3
issues: EXC-13
guide: requirements

When a person who may edit a project saves a requirement, Exciter shall keep it under the next key of the project and write an immutable revision whenever the title, statement, rationale, type or attribute values change.

Rationale: Reviews, links and baselines point at revisions; a revision that could change would make them mean nothing.

### FR-REQ-2 Give requirements types and attributes
type: Functional
priority: should
verification: Test
component: Requirements
refines: SN-3
relates: FR-REQ-1
issues: EXC-14
guide: requirements

When a project administrator defines a requirement type with attributes, Exciter shall offer them on the requirements of that type, check each value against its attribute definition, and keep the type and values each revision had.

Rationale: Each kind of specification has its own fields; a value that is not checked cannot be searched or counted.

### FR-REQ-3 Arrange requirements in a tree
type: Functional
priority: must
verification: Test
component: Requirements
refines: SN-3
relates: FR-REQ-1
issues: EXC-15
guide: requirements

When a person moves a requirement below another, Exciter shall keep its place in the tree, number the outline from the tree, and refuse a move that would make a requirement its own ancestor.

Rationale: A specification is read in order, and the numbers readers quote follow the tree.

### FR-REQ-4 Compare and restore revisions
type: Functional
priority: must
verification: Test
component: Requirements
refines: SN-1
relates: FR-REQ-1
issues: EXC-16
guide: requirements

When a person compares two revisions of a requirement, Exciter shall mark the words that were added and removed, and when a person restores one, Exciter shall write its content as the next revision and keep every earlier one.

Rationale: A change is judged by what it did, and an old wording must come back without losing the history.

### FR-REQ-5 Move a requirement to the trash and restore it
type: Functional
priority: should
verification: Test
component: Requirements
refines: SN-3
relates: FR-REQ-1
issues: EXC-13
guide: requirements

When a person trashes a requirement, Exciter shall take it out of the specification, the search and the coverage, keep it with its history, and restore it with its place and links when a person who may edit asks.

Rationale: Deleting by mistake is common and must be undone.
