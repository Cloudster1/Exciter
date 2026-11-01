# Functional requirements / Workflows

How a workflow is written, kept, checked and shared.

### FR-WFL-1 Save a workflow as checked YAML
type: Functional
priority: must
verification: Test
component: Workflows
refines: SN-2
issues: ROT-14, ROT-15
guide: workflows

When a person saves a workflow, Rotor shall check its YAML against the workflow schema and name each problem by line
and column, and shall refuse the save while a problem remains.

Rationale: A workflow that fails at its third node after twenty minutes costs more than one that is refused at once.

### FR-WFL-2 Keep every save as a version and compare versions
type: Functional
priority: must
verification: Test
component: Workflows
refines: SN-2
issues: ROT-15
guide: workflows

When a person saves a workflow, Rotor shall keep the save as a new numbered version that never changes, shall refuse a
save based on an older version, and shall show any two versions compared line by line.

Rationale: A run pins a version, so the version must stay what it was; two authors must not overwrite each other.

### FR-WFL-3 Share a workflow without sharing access
type: Functional
priority: must
verification: Test
component: Workflows
refines: SN-2, SN-4
issues: ROT-15
guide: workflows

When the author shares a workflow with groups or with everyone, Rotor shall let those people read and run it as
themselves, and shall never give them the author's connections.

Rationale: Sharing a workflow that wrote in the tools with its author's rights would be a way to borrow them.

### FR-WFL-4 Check a workflow against the tools when it is saved and when a run starts
type: Functional
priority: must
verification: Test
component: Workflows
refines: SN-2, SN-3
issues: ROT-90
guide: workflows

When a workflow is saved and when a run starts, Rotor shall read the tools afresh as the run's person and refuse a
workflow whose read binding allows a call that writes, and shall warn, without stopping the save, about a node that may
write after reading from a tool with no approval between.

Rationale: A tool may turn a call into a write after the workflow was saved; the check at the start catches it.

### FR-WFL-5 Draw and edit a workflow on a canvas
type: Functional
priority: should
verification: Demonstration
component: Workflows
refines: SN-2
issues: ROT-46
guide: workflows

When a person edits a workflow on the canvas, Rotor shall write every change into the YAML, shall change only the lines
the edit is about, and shall store no layout.

Rationale: The YAML is the workflow; a canvas that kept its own copy would drift from it.

### FR-WFL-6 Give an agent a model, a role and a step budget of its own
type: Functional
priority: should
verification: Test
component: Workflows
refines: SN-2, SN-7
issues: ROT-133
guide: workflows

Where an agent node names a model, a role or a step budget, Rotor shall run that agent with them, shall refuse a model
an administrator does not allow, and shall count each call at its own model's price toward the run's cost.

### FR-WFL-7 Run branches in parallel and bound loops
type: Functional
priority: should
verification: Test
component: Workflows
refines: SN-1, SN-2
issues: ROT-134, ROT-135
guide: workflows

Where a workflow has parallel branches, Rotor shall run them side by side and continue after all of them at a node that
joins them; where it loops, Rotor shall leave a node that has no visit left through the node named for that case.

Rationale: A loop without a bound runs until the workflow's step limit, which costs money and ends the run in an error.
