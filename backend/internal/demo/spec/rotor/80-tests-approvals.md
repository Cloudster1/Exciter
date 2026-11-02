# Test specification / Approvals

### TC-APR-1 A run waits at an approval without a sandbox
type: Test case
verifies: FR-APR-1
automated: yes
result: passed

Preconditions: A workflow with an approval node before a write.

| Action | Expected |
|---|---|
| Start the run. | The run stops at the approval. |
| List the run's sandboxes and tokens. | None is left. |
| Approve. | A fresh sandbox continues the run. |

### TC-APR-2 An approval shows the message, a preview and a form
type: Test case
verifies: FR-APR-2
automated: yes
result: passed

Preconditions: A run waiting at an approval with a form field `title`.

| Action | Expected |
|---|---|
| Open the approval. | The message, a preview of the writes before the next approval and the field `title` show. |
| Change the title and approve. | The next node reads the changed title. |

### TC-APR-3 Only the named people decide
type: Test case
verifies: FR-APR-3
automated: yes
result: passed

Preconditions: A run started by bob whose approval names the group `quality`, and carol, a member of it, and dave, who is not.

| Action | Expected |
|---|---|
| Dave tries to approve. | The decision is refused. |
| Carol approves. | The run continues. |

### TC-APR-4 A rejection or an expiry ends the run
type: Test case
verifies: FR-APR-4
automated: yes
result: passed

Preconditions: Two runs waiting at an approval, one with `expiresIn: 1m`.

| Action | Expected |
|---|---|
| Reject the first with a comment. | The run ends and shows the comment. |
| Wait two minutes for the second. | The run ends as expired. |

### TC-APR-5 The inbox lists what waits
type: Test case
verifies: FR-APR-5
automated: no
result: passed

Preconditions: Two approvals waiting for the tester.

| Action | Expected |
|---|---|
| Open any page. | The sidebar shows a count of 2 beside Approvals. |
| Open the inbox and choose one. | The approval opens. |

### TC-APR-6 A workflow that decides a review is refused
type: Test case
verifies: FR-APR-6
automated: yes
result: passed

Preconditions: A workflow whose Exciter binding allows the call that decides a review.

| Action | Expected |
|---|---|
| Save the workflow. | The save is refused and says a review is decided in the tool. |

### TC-APR-7 A write is held back at the tool until approved
type: Test case
verifies: FR-APR-7
automated: no
result: blocked
note: Not built yet; the tool stub does not hold writes back, so the test cannot run.

Preconditions: A binding with `approval: beforeWrite`.

| Action | Expected |
|---|---|
| Let a node call a writing tool before the approval. | The tool refuses it. |
| Approve, then repeat. | The tool accepts the write. |
