# Functional requirements / Traceability

How requirements are traced to each other.

### FR-TRC-1 Link two requirements with a typed trace link
type: Functional
priority: must
verification: Test
component: Traceability
refines: SN-1
issues: EXC-18
guide: links

When a person who may edit a requirement links it to another requirement, Exciter shall keep the link with the link
type the person picked and show it on both requirements, in the words that type gives each direction.

Rationale: A link type says how two requirements relate, which an untyped link cannot.

### FR-TRC-2 Mark the links of a changed requirement suspect
type: Functional
priority: must
verification: Test
component: Traceability
refines: SN-1
relates: FR-TRC-1
issues: EXC-36
guide: links

When the normative content of a requirement changes, Exciter shall mark every trace link that depends on it as
suspect until a person clears it.
