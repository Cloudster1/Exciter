# Functional requirements / Running

How a run starts, is followed, is stopped and is kept within bounds.

### FR-RUN-1 Start a workflow by hand
type: Functional
priority: must
verification: Test
component: Runs
refines: SN-1
issues: ROT-22
guide: running

When a person who may run a workflow starts it with inputs that fit its schema, Rotor shall queue a run of the
workflow's current version and carry it to its end in a sandbox of its own.

Rationale: One sandbox per run keeps a run's code and tokens apart from every other run's.

### FR-RUN-2 Follow a run live and cancel it
type: Functional
priority: must
verification: Test
component: Runs
refines: SN-1
issues: ROT-24, ROT-22
guide: running

While a run is going, Rotor shall show its graph coloured by what happened and tell the same events in words, and shall
end the run at the next step when its starter, the workflow's owner or an administrator cancels it.

### FR-RUN-3 Replace a sandbox that dies
type: Functional
priority: must
verification: Test
component: Runs
refines: SN-1
issues: ROT-19, ROT-21
guide: running

If a run's sandbox dies, Rotor shall start another and resume the run from its last checkpoint, up to three sandboxes
for one run.

Rationale: A run that needs an hour should survive a node being drained, but not loop on a sandbox that cannot work.

### FR-RUN-4 Stop a run that passes a limit
type: Functional
priority: must
verification: Test
component: Runs
refines: SN-7
issues: ROT-38
guide: running

When a run passes its workflow's limit of steps, time, tokens or cost, or makes more tool calls in a minute than allowed,
Rotor shall end the run and say which limit it passed; a run over the concurrency limit shall wait queued.

### FR-RUN-5 Refuse to start a run where no sandboxes run
type: Functional
priority: should
verification: Test
component: Runs
refines: SN-1
issues: ROT-178
guide: running

If the installation runs no sandboxes, Rotor shall refuse to start a run with a sentence that says so, and shall say so
on the workflow page before a person presses Start.

### FR-RUN-6 Read the content of a tool as data
type: Functional
priority: must
verification: Test
component: Runs
refines: SN-8
issues: ROT-40
guide: running

When an agent reads a value its prompt quotes or an answer of a tool, Rotor shall fence it as data, and shall remove a
link the agent writes unless it leads to one of the run's tools.

Rationale: A fence does not make a model immune, but it removes the cheapest ways to steer one.
