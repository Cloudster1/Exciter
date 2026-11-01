# Test specification / Search

### TC-SRC-1 Search ignores case and accents
type: Test case
verifies: FR-SRC-1
automated: yes
result: passed

Preconditions: A requirement titled `Sicherheit der Anmeldung` in a project the tester may view.

| Action | Expected |
|---|---|
| Search for `SICHERHEIT`. | The requirement is listed. |
| Search for the same word in a project the tester may not view. | Nothing from that project is listed. |

### TC-SRC-2 A query narrows the grid and a bad query is placed
type: Test case
verifies: FR-SRC-2
automated: yes
result: passed

Preconditions: A project with requirements in two statuses.

| Action | Expected |
|---|---|
| Enter the query `status = Approved`. | Only requirements in that status are listed. |
| Enter the query `status =`. | The query is refused and the position of the character it failed at is shown. |
