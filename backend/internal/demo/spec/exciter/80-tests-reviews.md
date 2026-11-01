# Test specification / Reviews

### TC-REV-1 A mention notifies the person
type: Test case
verifies: FR-REV-1
automated: yes
result: passed

Preconditions: Bob with in-app notifications on.

| Action | Expected |
|---|---|
| Alice comments on a requirement and mentions Bob. | Bob has one unread notification that opens the comment. |

### TC-REV-2 Only a named approver decides and an edit supersedes
type: Test case
verifies: FR-REV-2
automated: yes
result: passed

Preconditions: An approval rule naming Carol, and a requirement with a review pending.

| Action | Expected |
|---|---|
| Bob tries to approve. | The decision is refused. |
| Carol approves. | The requirement moves on. |
| Edit the requirement. | It is a new revision and the approval does not apply to it. |

### TC-REV-3 A signature needs a fresh proof
type: Test case
verifies: FR-REV-3
automated: yes
result: passed

Preconditions: A project with signed approvals and a review pending.

| Action | Expected |
|---|---|
| Decide without proving who you are again. | The decision is refused. |
| Confirm the meaning, prove with the password and decide. | The decision is kept with its signature, revision and hash. |

### TC-REV-4 A review session runs in rounds
type: Test case
verifies: FR-REV-4
automated: yes
result: passed

Preconditions: A section of three requirements and two reviewers.

| Action | Expected |
|---|---|
| Open a session on the section naming both reviewers. | Each requirement has a review for each of them. |
| Decide all, close the session. | A report is made with the decisions. |
