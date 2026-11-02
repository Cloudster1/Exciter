# Test specification / Workflow

### TC-WFL-1 A transition needs its starting status
type: Test case
verifies: FR-WFL-1
automated: yes
result: passed

Preconditions: A project with the default workflow and a requirement in its first status.

| Action | Expected |
|---|---|
| Take a transition that leaves another status. | It is refused. |
| Take a transition that leaves the requirement's status. | The status changes and the history shows the change. |

### TC-WFL-2 A locking status refuses changes
type: Test case
verifies: FR-WFL-2
automated: yes
result: passed

Preconditions: A status that locks, and a requirement in it.

| Action | Expected |
|---|---|
| Edit the statement, link the requirement and move it. | Each is refused with a sentence that says the requirement is locked. |
| Move the requirement to a status that does not lock. | It can be edited again. |

### TC-WFL-3 A required field blocks a status
type: Test case
verifies: FR-WFL-3
automated: yes
result: passed

Preconditions: A field required from the status Approved, empty on a requirement.

| Action | Expected |
|---|---|
| Move the requirement to Approved. | The move is refused and the sentence names the field. |
| Fill the field and move it again. | The requirement is Approved. |
