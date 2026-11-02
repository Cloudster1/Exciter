# Test specification / Git repositories

### TC-GIT-1 A merged branch stays on its issue
type: Test case
verifies: FR-GIT-1
automated: yes
result: passed

Preconditions: A connected repository and an issue ARM-1 with a branch and a pull request that was merged.

| Action | Expected |
|---|---|
| Open the issue's development panel. | It lists the branch, the pull request and the CI state. |
| Have the host delete the merged branch and open the panel again. | The branch is still listed as merged. |

### TC-GIT-2 A squash commit does not repeat a command
type: Test case
verifies: FR-GIT-2
automated: yes
result: passed

Preconditions: An issue in To Do and a branch with a commit whose message moves it to Done.

| Action | Expected |
|---|---|
| Push the commit. | The issue is Done. |
| Move the issue back to To Do and merge the pull request as a squash commit with the same message. | The issue stays in To Do. |

### TC-GIT-3 Disconnect asks first and is for administrators
type: Test case
verifies: FR-GIT-3
automated: no
result: passed

Preconditions: A connected repository, a project administrator and a user.

| Action | Expected |
|---|---|
| As the user, open the project's repositories. | Connect, Rotate secret and Disconnect are not shown. |
| As the administrator, choose Disconnect. | A dialog names the commits, branches and pull requests it removes. |
