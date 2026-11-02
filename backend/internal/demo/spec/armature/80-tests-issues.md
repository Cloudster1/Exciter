# Test specification / Issues

### TC-ISS-1 A parent two levels above is refused
type: Test case
verifies: FR-ISS-1
automated: yes
result: passed

Preconditions: A project with an epic, a story and a subtask.

| Action | Expected |
|---|---|
| Set the epic as the parent of the subtask. | Armature refuses and names the level the parent needs. |
| Set the story as the parent of the subtask. | The subtask shows the story as its parent. |

### TC-ISS-2 The later of two saves is refused
type: Test case
verifies: FR-ISS-2
automated: yes
result: passed

Preconditions: An issue open in two browsers, A and B, as two users.

| Action | Expected |
|---|---|
| In A change the summary and save. | The save succeeds. |
| In B change the description and save. | Armature refuses the save and asks B to reload. |
| Reload B. | B shows the summary A saved. |

### TC-ISS-3 Change the type to one with another workflow
type: Test case
verifies: FR-ISS-3
automated: yes
result: passed

Preconditions: A bug in status In Progress, and a story type whose workflow has no In Progress.

| Action | Expected |
|---|---|
| Change the type of the bug to story on its page. | The issue is a story and shows a status of the story workflow. |
| Open the history. | It records the type change and the status move. |

### TC-ISS-4 Bulk edit leaves a refused issue as it was
type: Test case
verifies: FR-ISS-4
automated: yes
result: passed

Preconditions: Three issues ticked in a list, one of which the tester may not edit.

| Action | Expected |
|---|---|
| Bulk edit the label `label`, adding it to all three. | Two issues carry the label. |
| Read the answer. | It names the third issue and why it was left as it was. |

Parameters: label = urgent, blocked
