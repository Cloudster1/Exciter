# Test specification / Notifications and automation

### TC-NTF-1 Turning the inbox off keeps the mail
type: Test case
verifies: FR-NTF-1
automated: yes
result: passed

Preconditions: A user who has mail on for comments, and a colleague who comments on the user's issue.

| Action | Expected |
|---|---|
| Turn the inbox off for comments. | The setting is saved. |
| Have the colleague comment. | No inbox entry appears, and the mail arrives. |

### TC-NTF-2 A failed delivery is tried again
type: Test case
verifies: FR-NTF-2
automated: yes
result: passed

Preconditions: A webhook endpoint that answers 500 once and then 200.

| Action | Expected |
|---|---|
| Create an issue. | The log shows a failed first attempt. |
| Wait for the first retry. | The log shows a second attempt that succeeded, with the same event id. |

### TC-NTF-3 A rule run by hand checks its condition
type: Test case
verifies: FR-NTF-3
automated: yes
result: passed

Preconditions: A rule that sets a field only for issues of type Bug.

| Action | Expected |
|---|---|
| Run the rule by hand on a Task. | Nothing changes, and the log says why. |
| Run it on a Bug. | The field is set. |
