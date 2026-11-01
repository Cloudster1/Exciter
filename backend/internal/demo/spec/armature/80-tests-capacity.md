# Test specification / Calendars and capacity

### TC-CAP-1 An imported holiday keeps its length
type: Test case
verifies: FR-CAP-1
automated: yes
result: passed

Preconditions: A holiday calendar and an .ics file with an event of `days` days.

| Action | Expected |
|---|---|
| Import the file. | The calendar holds one holiday of that length. |
| Import the same file again. | The calendar holds the same holiday, not doubled. |

Parameters: Parameters: days = 1, 2, 3

### TC-CAP-2 A team manager changes a member's absence
type: Test case
verifies: FR-CAP-2
automated: yes
result: passed

Preconditions: A team with a manager and a member who recorded two days away.

| Action | Expected |
|---|---|
| As the manager, move the absence by one day. | The absence shows the new days. |
| As another colleague, open the project calendar. | The days show, and no reason. |

### TC-CAP-3 The resources page lists unplaceable work
type: Test case
verifies: FR-CAP-3
automated: yes
result: passed

Preconditions: A scrum project with a team of capacity 40 hours a week and one issue without an estimate.

| Action | Expected |
|---|---|
| Open the resources page. | Each week shows scheduled hours against hours left for the team. |
| Read the list of unplaced work. | It names the issue without an estimate and opens it on click. |
