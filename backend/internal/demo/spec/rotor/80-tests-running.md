# Test specification / Running

### TC-RUN-1 Start a run and follow it to its end
type: Test case
verifies: FR-RUN-1
automated: yes
result: passed

Preconditions: A workflow the tester may run, with an input `project`.

| Action | Expected |
|---|---|
| Start the workflow with `project` set to `CP`. | A run is queued. |
| Wait for the run. | The run ends as succeeded. |
| Start it again with an input that does not fit the schema. | No run starts and the dialog names the input. |

### TC-RUN-2 Follow a run live and cancel it
type: Test case
verifies: FR-RUN-2
automated: yes
result: passed

Preconditions: A workflow with a node that waits 60 seconds.

| Action | Expected |
|---|---|
| Start the run and open its page. | The graph colours the finished nodes and the events appear in words beside it. |
| Cancel the run as a person who started it. | The run ends as cancelled at the next step. |

Parameters: canceller = starter, owner, administrator

### TC-RUN-3 A sandbox that dies is replaced
type: Test case
verifies: FR-RUN-3
automated: yes
result: failed
note: After the second sandbox died the run resumed from the first checkpoint instead of the last one.

Preconditions: A run of three nodes in the Docker backend, and a way to kill its sandbox.

| Action | Expected |
|---|---|
| Kill the sandbox after the second node. | Another sandbox starts and the run goes on at the third node. |
| Kill it twice more. | The run ends in an error that says it used three sandboxes. |

### TC-RUN-4 A run over a limit stops and says which
type: Test case
verifies: FR-RUN-4
automated: yes
result: passed

Preconditions: A workflow with a limit `limit` set low enough to pass.

| Action | Expected |
|---|---|
| Start a run that passes the limit. | The run ends and names the limit it passed. |
| Start three runs of a workflow with `maxConcurrent: 1`. | Two wait queued and start as the others end. |

Parameters: limit = maxSteps, timeout, maxTokens, maxCost, toolCallsPerMinute

### TC-RUN-5 A run is refused where no sandboxes run
type: Test case
verifies: FR-RUN-5
automated: yes
result: passed

Preconditions: An installation started without sandboxes.

| Action | Expected |
|---|---|
| Open a workflow's page. | It says the installation runs no workflows. |
| Start a run through the API. | The answer is 409 `runs_off` with a sentence that says so. |

### TC-RUN-6 Planted text is read as data
type: Test case
verifies: FR-RUN-6
automated: yes
result: passed

Preconditions: An issue whose description tells the reader to open a link to a host that is none of the run's tools.

| Action | Expected |
|---|---|
| Run an agent that quotes the description in its prompt. | The prompt holds the description inside an `untrusted-data` fence. |
| Let the agent answer with the link. | The link reads `[link removed]` and the timeline says so. |
