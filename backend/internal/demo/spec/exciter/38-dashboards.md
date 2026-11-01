# Functional requirements / Dashboards

How the state of a project is seen and values are computed.

### FR-DSH-1 Show dashboards of charts and counters
type: Functional
priority: should
verification: Test
component: Dashboards
refines: SN-12
issues: EXC-212
guide: dashboards

When a person opens a dashboard, Exciter shall draw its charts and counters live over the requirements the person may view, and open the requirements behind a figure in the grid.

Rationale: A figure nobody can check is not trusted.

### FR-DSH-2 Compute attributes
type: Functional
priority: could
verification: Test
component: Dashboards
refines: SN-12
relates: FR-DSH-1
issues: EXC-213
guide: dashboards

When a project administrator gives an attribute a formula over a requirement's own fields, its children or its linked requirements, Exciter shall keep the value outside the revisions and work it out again when its inputs change.

Rationale: A roll-up kept by hand is wrong by the next change.
