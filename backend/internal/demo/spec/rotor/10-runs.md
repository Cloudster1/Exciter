# Functional requirements / Runs

### FR-RUN-1 Start a workflow by hand
type: Functional
priority: must
verification: Test
component: Runs
refines: SN-1
issues: ROT-22
guide: concepts

When a person who may run a workflow starts it, Rotor shall queue a run of the workflow's current version and show
the run's state until it ends.
