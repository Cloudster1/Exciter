# Test specification / Access

### TC-ACC-1 A role allows what it lists and no more
type: Test case
verifies: FR-ACC-1
automated: yes
result: passed

Preconditions: One person per role, and a project.

| Action | Expected |
|---|---|
| As `role`, try to edit an issue, complete a sprint, rename a board and create a project. | Only the actions the role lists succeed. |

Parameters: Parameters: role = reader, user, scrum master, project administrator, global administrator

### TC-ACC-2 An issue behind the wall answers not found
type: Test case
verifies: FR-ACC-2
automated: yes
result: passed

Preconditions: An issue in a project the tester has no role in.

| Action | Expected |
|---|---|
| Open the issue. | Armature answers not found. |
| Ask for access. | The answer is the same as for a key that does not exist, and the project's administrators are told. |

### TC-ACC-3 A reset answers the same for every address
type: Test case
verifies: FR-ACC-3
automated: yes
result: passed

Preconditions: A known and an unknown address.

| Action | Expected |
|---|---|
| Ask for a link for each address. | Both answers read the same. |
| Check the mail. | Only the known address received a link. |

### TC-ACC-4 Two-step sign-in is required and a recovery code works
type: Test case
verifies: FR-ACC-4
automated: yes
result: passed

Preconditions: An installation that requires two-step sign-in for everyone, and a user with an app and recovery codes.

| Action | Expected |
|---|---|
| Sign in as a user without a second step. | Armature asks to add one. |
| Sign in as the user with the app and enter a recovery code. | The sign-in succeeds, and the code cannot be used again. |

### TC-ACC-5 Concurrent revokes leave an administrator
type: Test case
verifies: FR-ACC-5
automated: yes
result: passed

Preconditions: Two global administrators.

| Action | Expected |
|---|---|
| Revoke both at the same time. | One revoke is refused. |
| List the administrators. | One is active. |

### TC-ACC-6 A provider group lets a person in
type: Test case
verifies: FR-ACC-6
automated: no
result: passed

Preconditions: The Keycloak realm with a group that holds the user role, and a person in it who has no account.

| Action | Expected |
|---|---|
| Sign in through the provider. | The person is let in as a member with the user role. |
| Open the group Everyone. | It lists the person. |
