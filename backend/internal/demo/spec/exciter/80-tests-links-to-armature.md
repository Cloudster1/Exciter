# Test specification / Armature links

### TC-ARM-1 Saving the connection reads what Armature offers
type: Test case
verifies: FR-ARM-1
automated: yes
result: passed

Preconditions: An Armature that does not list `remote-links@1`.

| Action | Expected |
|---|---|
| Save the connection as an administrator. | The settings say that putting requirements on their issues is off and to update Armature. |

### TC-ARM-2 Link a requirement to an issue with a relation
type: Test case
verifies: FR-ARM-2
automated: yes
result: passed

Preconditions: A connected Armature with an issue and a requirement.

Parameters: relation = implements, verifies, relates

| Action | Expected |
|---|---|
| Link the requirement to the issue as `relation`. | The issue shows with its status in the requirement's panel. |
| Wait for the sync, then open the issue in Armature. | It carries a remote link from Exciter with the relation `relation`. |

### TC-ARM-3 Create an issue from a requirement
type: Test case
verifies: FR-ARM-3
automated: yes
result: blocked
note: Armature's stack was down on the day of the run; run again when it is up.

Preconditions: A connected Armature and a requirement.

| Action | Expected |
|---|---|
| Create an issue from the requirement in the project `DEMO`. | The issue exists in Armature and is linked. |

### TC-ARM-4 A webhook refreshes the status and a bad signature is refused
type: Test case
verifies: FR-ARM-4
automated: yes
result: passed

Preconditions: A requirement linked to an open issue.

| Action | Expected |
|---|---|
| Post a webhook that closes the issue, signed with the connection's secret. | The link shows the issue as done. |
| Post the same with a wrong signature. | The post is refused and nothing changes. |
