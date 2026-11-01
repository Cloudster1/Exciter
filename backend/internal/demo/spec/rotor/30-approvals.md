# Functional requirements / Approvals

How a run stops for a person before it writes.

### FR-APR-1 Hold a run at an approval and release its sandbox
type: Functional
priority: must
verification: Test
component: Approvals
refines: SN-3, SN-4
issues: ROT-31
guide: approvals

When a run reaches an approval node, Rotor shall stop the run, shall remove its sandbox and revoke its tokens, and shall
continue it in a fresh sandbox when the approval is granted.

Rationale: A run waiting three days for a person must hold neither a process nor a token meanwhile.

### FR-APR-2 Show what the run is about to write
type: Functional
priority: must
verification: Test
component: Approvals
refines: SN-3
issues: ROT-31
guide: approvals

When a person opens an approval, Rotor shall show its message, a preview of the writes the run may make before the next
approval, and a form of the values an approver may change.

### FR-APR-3 Let only the right people decide
type: Functional
priority: must
verification: Test
component: Approvals
refines: SN-3, SN-4
issues: ROT-31
guide: approvals

When a person decides an approval, Rotor shall accept the decision only from the person who started the run or, where the
node names a group, from a member of that group.

### FR-APR-4 End the run on a rejection or an expiry
type: Functional
priority: must
verification: Test
component: Approvals
refines: SN-3
issues: ROT-31
guide: approvals

When an approval is rejected, or nobody decides it within its time, 72 hours unless the node says otherwise and at most
168, Rotor shall end the run and record why.

### FR-APR-5 List what waits for a person
type: Functional
priority: should
verification: Demonstration
component: Approvals
refines: SN-3
issues: ROT-31
guide: approvals

While approvals wait for a person, Rotor shall list them in an inbox with a count in the sidebar, each linking to the
approval.

### FR-APR-6 Never decide a review for a person
type: Functional
priority: must
verification: Inspection
component: Approvals
refines: SN-3, SN-4
issues: ROT-31
guide: approvals

Rotor shall refuse a workflow whose binding allows a call that decides a review in Exciter, so that a review is decided
by a person in the tool itself.

### FR-APR-7 Confirm writes at the tool before they happen
type: Functional
priority: should
verification: Test
component: Approvals
refines: SN-3
issues: ROT-32
guide: approvals

Where a binding asks for approval before writes, Rotor shall hold a write back at the tool until the approval is granted,
so that a node cannot reach it by another way.

Rationale: An approval only the runner enforces is only as strong as the sandbox; the tool is the place to hold it.
