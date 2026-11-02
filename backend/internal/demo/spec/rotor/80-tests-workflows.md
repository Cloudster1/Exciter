# Test specification / Workflows

### TC-WFL-1 A workflow with a problem is refused with its position
type: Test case
verifies: FR-WFL-1
automated: yes
result: passed

Preconditions: A tester who may write workflows, and a workflow whose second node names an edge to a node that does not exist.

| Action | Expected |
|---|---|
| Save the workflow. | The save is refused. |
| Read the problem. | It names the line and column of the edge and says what to do. |
| Correct the edge and save. | The workflow is saved as version 1. |

### TC-WFL-2 Versions never change and two can be compared
type: Test case
verifies: FR-WFL-2
automated: yes
result: passed

Preconditions: A workflow with two versions, and a second tester who opened version 2 before the first tester saved version 3.

| Action | Expected |
|---|---|
| The first tester saves version 3 from version 2. | Version 3 exists; version 2 reads as it did. |
| The second tester saves from version 2. | The save is refused because it is based on an older version. |
| Compare versions 1 and 3. | The changed lines show, line by line. |

### TC-WFL-3 A shared workflow runs with the runner's own access
type: Test case
verifies: FR-WFL-3
automated: yes
result: passed

Preconditions: A workflow shared with the group `engineering`, written by alice, who is connected to Exciter, and bob, a member of the group, who is not.

| Action | Expected |
|---|---|
| Bob opens the workflow. | He can read and run it. |
| Bob starts a run whose first node reads Exciter. | The run ends in an error that says bob has not connected Exciter. |

### TC-WFL-4 A read binding that allows a writing call is refused
type: Test case
verifies: FR-WFL-4
automated: yes
result: passed

Preconditions: A workflow whose binding says it only reads and allows a tool that creates a requirement.

| Action | Expected |
|---|---|
| Save the workflow. | The save is refused and names the binding and the tool. |
| Make the tool create requirements after a read with no approval between, in a binding that may write. | The save succeeds with the warning `untrusted_then_write`. |
| Start a run after the tool turned a call into a write. | The run is refused at its start with the same problem. |

### TC-WFL-5 A change on the canvas changes only its lines
type: Test case
verifies: FR-WFL-5
automated: no
result: passed

Preconditions: A workflow whose YAML has comments and a block-style prompt.

| Action | Expected |
|---|---|
| Open the Canvas tab and rename a node. | The graph shows the new name. |
| Open the YAML tab. | The node, the edges and templates that name it carry the new name; comments and style are as they were. |

### TC-WFL-6 An agent runs on its own model and an unlisted model is refused
type: Test case
verifies: FR-WFL-6
automated: yes
result: passed

Preconditions: Two models an administrator allows, and a workflow with one agent naming the second.

| Action | Expected |
|---|---|
| Start a run. | The run's page shows the agent ran on the second model. |
| Change the agent's model to one not allowed and save. | The save is refused with `model_not_allowed`. |

### TC-WFL-7 Branches join and a loop leaves by its exhaustion node
type: Test case
verifies: FR-WFL-7
automated: yes
result: passed

Preconditions: A workflow with two parallel branches joined with `join: all`, and a loop whose node has `maxVisits: 2` and an `onExhausted` node.

| Action | Expected |
|---|---|
| Start a run. | Both branches run and the joined node runs once, after both. |
| Read the loop's events. | The third visit goes to the `onExhausted` node. |
