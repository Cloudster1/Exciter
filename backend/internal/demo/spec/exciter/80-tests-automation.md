# Test specification / Automation

### TC-AUT-1 A read-only token refuses writes
type: Test case
verifies: FR-AUT-1
automated: yes
result: passed

Preconditions: A read-only personal access token of alice.

| Action | Expected |
|---|---|
| Read a requirement with the token. | The requirement is returned. |
| Change it with the token. | The change is refused as read only. |

### TC-AUT-2 An assistant's write is marked
type: Test case
verifies: FR-AUT-2
automated: yes
result: passed

Preconditions: An assistant connected by OAuth for alice.

| Action | Expected |
|---|---|
| Create a requirement with the MCP tool. | It is made as alice. |
| Open its history. | The revision shows the app's name beside alice. |

### TC-AUT-3 A webhook is signed and retried
type: Test case
verifies: FR-AUT-3
automated: yes
result: passed

Preconditions: An endpoint that answers 500, subscribed to requirement events.

| Action | Expected |
|---|---|
| Change a requirement. | A signed event is posted and the delivery is retried later. |
| Let the endpoint answer 200. | The next retry succeeds and the delivery is recorded as sent. |
