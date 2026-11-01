# Test specification / Non-functional

### TC-NFR-1 Check a workflow of 50 nodes
type: Test case
verifies: NFR-1
automated: yes
result: passed

Preconditions: The development stack and a workflow of 50 nodes.

| Action | Expected |
|---|---|
| Validate the workflow 200 times in a row. | The 95th percentile is under 1 second. |

### TC-NFR-2 Events show within 2 seconds
type: Test case
verifies: NFR-2
automated: yes
result: failed
note: The 95th percentile was 2.6 seconds on the shared runner; the events were delayed by the stub model.

Preconditions: The development stack and a run that emits 100 events.

| Action | Expected |
|---|---|
| Follow the run's stream and compare each event's time with its arrival. | The 95th percentile of the delay is under 2 seconds. |

### TC-NFR-3 A delivery is answered at once
type: Test case
verifies: NFR-3
automated: yes
result: passed

Preconditions: Ten triggers on one topic of the same tool.

| Action | Expected |
|---|---|
| Deliver 500 signed events. | Each answer is 202 and the 99th percentile is under 1 second. |

### TC-NFR-4 Run data is ciphertext
type: Test case
verifies: NFR-4
automated: yes
result: passed

Preconditions: A finished run and a database role that may read the tables.

| Action | Expected |
|---|---|
| Read the inputs, output, events, checkpoints and approvals of the run. | Every value is ciphertext; none reads as the run's text. |

### TC-NFR-5 A token is refused after the run
type: Test case
verifies: NFR-5
automated: yes
result: passed

Preconditions: A run holding a token for a tool.

| Action | Expected |
|---|---|
| End the run with `end`. | Within 10 seconds the tool refuses the token. |

Parameters: end = success, cancellation, approval wait

### TC-NFR-6 Finished runs are deleted at their retention
type: Test case
verifies: NFR-6
automated: yes
result: passed

Preconditions: A workflow with `retention: 1d` and a run finished two days ago.

| Action | Expected |
|---|---|
| Wait for the sweep. | The run's data is gone. |
| Read the audit log. | It records how many runs of the workflow were deleted. |
