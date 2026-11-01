# Functional requirements / Workflow

How a requirement moves through the statuses of a project.

### FR-WFL-1 Move a requirement along the workflow
type: Functional
priority: must
verification: Test
component: Workflow
refines: SN-5
issues: EXC-17
guide: workflow

When a person moves a requirement through a transition of its workflow, Exciter shall change its status only if the transition leaves the current status and the person meets the transition's conditions, and shall log the change.

Rationale: The status says what may be done with a requirement; it must only change by the rules the project agreed.

### FR-WFL-2 Lock a requirement by its status or by hand
type: Functional
priority: should
verification: Test
component: Workflow
refines: SN-6
relates: FR-WFL-1
issues: EXC-205
guide: workflow

While a requirement stands in a status that locks, or a person has locked it, Exciter shall refuse a new revision, a move, a link and the trash for it, for every writer.

Rationale: A released requirement that can still change is not released.

### FR-WFL-3 Require fields and rules at a status
type: Functional
priority: should
verification: Test
component: Workflow
refines: SN-5
relates: FR-WFL-1
issues: EXC-206
guide: workflow

When a transition or a save would leave a field empty that the status requires, or would break a dependent value, a role-only field or a trace condition, Exciter shall refuse it with a sentence that names the field and what to do.

Rationale: A project decides what a requirement must hold before it is reviewed or released.
