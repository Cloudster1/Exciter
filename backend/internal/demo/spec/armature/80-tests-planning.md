# Test specification / Planning

### TC-PLN-1 Carry the unfinished work into the next sprint
type: Test case
verifies: FR-PLN-1
automated: yes
result: passed

Preconditions: A running sprint with one done and one open issue, and a planned next sprint of the same team.

| Action | Expected |
|---|---|
| Complete the sprint and choose the next sprint for the unfinished issue. | The sprint is completed. |
| Open the next sprint. | It holds the open issue. |
| Open the completed sprint's report. | It shows two issues, one done, as at completion. |

### TC-PLN-2 The burndown has a point on the first day
type: Test case
verifies: FR-PLN-2
automated: yes
result: passed

Preconditions: A sprint that started today with five issues.

| Action | Expected |
|---|---|
| Open the sprint's burndown. | It shows a point for today with five issues remaining. |
| File a sixth issue into the sprint tomorrow and open the burndown. | The sixth issue counts from tomorrow, not from today. |

### TC-PLN-3 A circle of blocks is refused
type: Test case
verifies: FR-PLN-3
automated: yes
result: passed

Preconditions: Issues A and B where A blocks B.

| Action | Expected |
|---|---|
| Add the link that B blocks A. | Armature refuses and says it would be a circle. |
| Make A depend on a parent P of B. | P and its children are held back after A. |

### TC-PLN-4 Preview, apply and undo an automatic plan
type: Test case
verifies: FR-PLN-4
automated: yes
result: passed

Preconditions: A timeline with issue A blocking B, B dated before A ends.

| Action | Expected |
|---|---|
| Choose Auto plan. | A preview lists B moving after A. |
| Apply the plan. | B starts after A ends. |
| Undo the plan. | B has its first dates again. |
