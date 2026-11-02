# Test specification / Baselines

### TC-BSL-1 Take a baseline and read it after a change
type: Test case
verifies: FR-BSL-1
automated: yes
result: passed

Preconditions: A project of five requirements.

| Action | Expected |
|---|---|
| Take a baseline named `R1`. | It lists five requirements with their revisions. |
| Edit one requirement, then read the baseline. | It still shows the old revision. |

### TC-BSL-2 Compare a baseline with the present
type: Test case
verifies: FR-BSL-2
automated: yes
result: passed

Preconditions: A baseline `R1` and later one edit, one added and one trashed requirement.

| Action | Expected |
|---|---|
| Compare `R1` with the present specification. | The comparison lists one changed, one added and one removed requirement. |

### TC-BSL-3 A signed baseline never changes
type: Test case
verifies: FR-BSL-3
automated: yes
result: passed

Preconditions: A baseline signed by all the people the project names.

| Action | Expected |
|---|---|
| Rename the baseline. | It is refused as signed. |
| Delete it. | It is refused as signed. |
