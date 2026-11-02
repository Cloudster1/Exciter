# Test specification / Risks

### TC-RSK-1 A rating gives a level
type: Test case
verifies: FR-RSK-1
automated: yes
result: passed

Preconditions: A project with a risk scheme of a three by three matrix.

| Action | Expected |
|---|---|
| Rate a risk with probability 3 and severity 3. | The level before mitigation is the one the matrix gives for that cell. |
| Rate it after mitigation with probability 1. | The level after mitigation is lower and both are in the revision. |

### TC-RSK-2 Only a mitigation in effect lowers a risk
type: Test case
verifies: FR-RSK-2
automated: yes
result: passed

Preconditions: A risk and a mitigation requirement linked to it, not yet implemented.

| Action | Expected |
|---|---|
| Open the risk matrix. | The risk stands at its level before mitigation. |
| Implement and verify the mitigation, then open the matrix. | The risk stands at its level after mitigation. |
