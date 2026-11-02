# Test specification / Assistants

### TC-AST-1 An assistant connects and works as the person
type: Test case
verifies: FR-AST-1
automated: yes
result: passed

Preconditions: An assistant that speaks MCP and a tester.

| Action | Expected |
|---|---|
| Add Rotor's endpoint to the assistant and call a tool. | Rotor's consent page opens and names the assistant. |
| Allow it and list workflows. | It lists the tester's workflows. |
| Start a run. | The run starts as the tester. |
| Connect another assistant with Only read and start a run. | The start is refused. |

### TC-AST-2 An assistant cannot decide an approval
type: Test case
verifies: FR-AST-2
automated: yes
result: passed

Preconditions: A connected assistant that may act, and a run waiting at an approval.

| Action | Expected |
|---|---|
| Ask the assistant for the approval. | It reads the approval. |
| Ask it to approve. | It has no tool for it, and a decision sent to Rotor with its token is refused. |

### TC-AST-3 A person disconnects an assistant
type: Test case
verifies: FR-AST-3
automated: no
result: passed

Preconditions: A connected assistant, and an administrator.

| Action | Expected |
|---|---|
| Open Settings and Connected assistants. | The assistant shows with what it may do and when it was last used. |
| Disconnect it. | Its next call is refused. |
| As an administrator, switch assistants off for the installation. | Every assistant's token is refused until they are switched on again. |
