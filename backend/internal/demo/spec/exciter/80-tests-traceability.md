# Test specification / Traceability

### TC-TRC-1 Link two requirements and read the link from both
type: Test case
verifies: FR-TRC-1
automated: yes
result: passed

Preconditions: A project with two requirements the tester may edit.

| Action | Expected |
|---|---|
| Link the first requirement to the second with the type Refines. | The first requirement lists the link as refines. |
| Open the second requirement. | It lists the link as is refined by. |

### TC-TRC-2 A changed requirement makes its links suspect
type: Test case
verifies: FR-TRC-2, FR-TRC-1
automated: yes
result: passed

Preconditions: Two requirements linked by Refines.

| Action | Expected |
|---|---|
| Change the statement of the second requirement and save it. | The link shows as suspect on both requirements. |
| Clear the suspect mark on the first requirement. | The link no longer shows as suspect. |

### TC-TRC-3 The trace counts what the reader may not see
type: Test case
verifies: FR-TRC-3
automated: yes
result: passed

Preconditions: A requirement linked to one requirement in a project the tester may not view.

| Action | Expected |
|---|---|
| Open the trace of the requirement. | The visible links are drawn. |
| Read the count of hidden links. | One link is counted and nothing names its other end. |
