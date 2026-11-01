# Test specification / Boards

### TC-BRD-1 Rename a board and find it under its new name
type: Test case
verifies: FR-BRD-1
automated: yes
result: passed

Preconditions: A project with two boards, Team A and Team B, and a tester who administers the project.

| Action | Expected |
|---|---|
| Rename the board Team A to Platform. | The board's heading reads Platform. |
| Open the project's list of boards. | It lists Platform and Team B. |

### TC-BRD-2 Swimlanes change only the board being viewed
type: Test case
verifies: FR-BRD-2
automated: no
result: failed
note: The swimlanes of the second board changed the first board as well.

Preconditions: A project with two boards and a tester who administers it.

| Action | Expected |
|---|---|
| Open the second board and configure swimlanes by assignee. | The second board shows a lane per assignee. |
| Open the first board. | The first board shows no swimlanes. |

### TC-BRD-3 Old finished work leaves the kanban board
type: Test case
verifies: FR-BRD-3
automated: yes
result: passed

Preconditions: A kanban board that keeps finished work for 7 days, with one issue finished 10 days ago and one finished today.

| Action | Expected |
|---|---|
| Open the board. | Only the issue finished today shows in the done lane. |
| Read the done lane's work-in-progress count. | It counts the one issue. |

### TC-BRD-4 A quick filter shows only matching cards
type: Test case
verifies: FR-BRD-4
automated: yes
result: passed

Preconditions: A board with cards of two assignees.

| Action | Expected |
|---|---|
| Choose the quick filter Mine. | Only the tester's cards show. |
| Clear the filter. | All cards show again. |
