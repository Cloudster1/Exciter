# Non-functional requirements

### NFR-1 A workflow is checked quickly
type: Non-functional
priority: should
verification: Test
component: Workflows
refines: SN-2
issues: ROT-15
guide: workflows

Rotor shall answer the check of a workflow of 50 nodes within 1 second at the 95th percentile, with the stack running
on the development compose file.

| Nodes | Answer within |
|---|---|
| 50 | 1 s |

### NFR-2 A run's events show while they happen
type: Non-functional
priority: should
verification: Test
component: Runs
refines: SN-1
issues: ROT-24
guide: running

Rotor shall show an event of a run on its page within 2 seconds of the event at the 95th percentile.

| Percentile | Delay within |
|---|---|
| 95th | 2 s |

### NFR-3 A tool's event is kept at once
type: Non-functional
priority: should
verification: Test
component: Triggers
refines: SN-5
issues: ROT-35
guide: triggers

Rotor shall answer a signed delivery of a tool with 202 within 1 second at the 99th percentile, however many triggers
match, and shall start the matching runs after the answer.

| Percentile | Answer within |
|---|---|
| 99th | 1 s |

### NFR-4 A run's data is sealed at rest
type: Non-functional
priority: must
verification: Inspection
component: Runs
refines: SN-4, SN-8
issues: ROT-41, ROT-91
guide: running

Rotor shall store the inputs, output, events, checkpoints and approvals of every run sealed with a data key of the run,
so that 100 percent of the rows inspected hold ciphertext.

### NFR-5 No token outlives its run
type: Non-functional
priority: must
verification: Test
component: Connections
refines: SN-4
issues: ROT-23, ROT-12
guide: connections

Rotor shall revoke every token a run holds within 10 seconds of the run's end, so that the tool refuses it afterwards.

| Event | Token refused within |
|---|---|
| Run ends, is cancelled or waits at an approval | 10 s |

### NFR-6 Finished runs are deleted on time
type: Non-functional
priority: should
verification: Test
component: Administration
refines: SN-7
issues: ROT-41
guide: administration

Rotor shall delete the data of a finished run when its retention has passed, 90 days unless the workflow or an
administrator says fewer, and shall keep the audit log's record of the run.

| Setting | Data kept for |
|---|---|
| Installation default | 90 days |
| Workflow maximum | 3,650 days |
