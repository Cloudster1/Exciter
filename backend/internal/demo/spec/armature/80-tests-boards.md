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

### TC-IF-1 A remote link shows on its issue
type: Test case
verifies: IF-1
automated: yes
result: passed

Preconditions: An issue the tester may edit, and a token that may write it.

| Action | Expected |
|---|---|
| Add a remote link to the issue through the REST API, with relation `relation`. | The API answers 201 with the link. |
| Open the issue. | The link shows under its relation with its title. |

Parameters: relation = implements, verifies
