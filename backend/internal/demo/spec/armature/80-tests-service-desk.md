# Test specification / Service desk

### TC-DSK-1 A mailed request becomes an issue and threads
type: Test case
verifies: FR-DSK-1
automated: no
result: blocked
note: The test mailbox is not available on the build runner until the mail server is set up there.

Preconditions: A desk with a mailbox the desk can read.

| Action | Expected |
|---|---|
| Send a mail to the desk's address. | A request exists with the mail's subject. |
| Reply to the desk's answer. | The reply is added to the same request. |

### TC-DSK-2 An internal note never reaches the customer
type: Test case
verifies: FR-DSK-2
automated: yes
result: passed

Preconditions: A request with a customer and an agent.

| Action | Expected |
|---|---|
| As the agent, add an internal note. | The note shows in the agent's view. |
| Open the request in the customer's portal. | The note is not shown. |

### TC-DSK-3 Resolving without a reply stops the first-response clock
type: Test case
verifies: FR-DSK-3
automated: yes
result: passed

Preconditions: A request whose first response is due in two hours.

| Action | Expected |
|---|---|
| Resolve the request with no public reply. | The first-response clock stops. |
| Change the priority of another running request. | Its due times are recalculated. |

### TC-DSK-4 A customer's reply reopens, an automatic one does not
type: Test case
verifies: FR-DSK-4
automated: yes
result: passed

Preconditions: A resolved request.

| Action | Expected |
|---|---|
| Reply as the customer. | The request moves back to the status the workflow names. |
| Resolve it again and send an out-of-office reply from the customer's address. | The request stays resolved. |
