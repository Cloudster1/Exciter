# Test specification / Dashboards

### TC-DSH-1 A chart counts what the reader may view
type: Test case
verifies: FR-DSH-1
automated: yes
result: passed

Preconditions: A dashboard of requirements by status, and a project with one requirement the reader may not view.

| Action | Expected |
|---|---|
| Open the dashboard as the reader. | The counts leave out the hidden requirement. |
| Select a bar. | The grid lists exactly the requirements it counts. |

### TC-DSH-2 A computed attribute follows its inputs
type: Test case
verifies: FR-DSH-2
automated: yes
result: failed
note: The day-based value still showed the previous day at the second step; the sum worked.

Preconditions: An attribute with the formula sum of the children's Cost, and an attribute that uses today().

| Action | Expected |
|---|---|
| Change a child's Cost. | The parent's value changes. |
| Read the day-based attribute the next day. | The value is of the new day. |
