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

### TC-NFR-1 Coverage of a large project
type: Test case
verifies: NFR-1
automated: yes
result: blocked
note: The load fixture of 5,000 requirements was not available on the test stack.

Preconditions: A project of 5,000 requirements on the development stack.

| Action | Expected |
|---|---|
| Ask the project's coverage 100 times. | The 95th percentile of the answers is within 2 seconds. |
