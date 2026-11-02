# Test specification / Requirements

### TC-REQ-1 Edit a requirement and read its revisions
type: Test case
verifies: FR-REQ-1, FR-REQ-4
automated: yes
result: passed

Preconditions: A project with a requirement at revision 1.

| Action | Expected |
|---|---|
| Change the statement and save. | The requirement is at revision 2 and revision 1 is unchanged. |
| Compare revision 1 with revision 2. | Added words are marked as inserted and removed words as deleted. |
| Restore revision 1. | Revision 3 holds the content of revision 1 and revisions 1 and 2 remain. |

### TC-REQ-2 A required attribute is checked
type: Test case
verifies: FR-REQ-2
automated: yes
result: passed

Preconditions: A type with a number attribute `Cost`.

| Action | Expected |
|---|---|
| Save a requirement of the type with the text `abc` as Cost. | The save is refused with a sentence that says Cost takes a number. |
| Save it with `12`. | The value is kept and shown on the requirement. |

### TC-REQ-3 Move a requirement in the tree
type: Test case
verifies: FR-REQ-3
automated: yes
result: passed

Preconditions: Three requirements, B below A and C below B.

| Action | Expected |
|---|---|
| Move A below C. | The move is refused because A would be its own ancestor. |
| Move C below A. | C stands below A and the outline numbers follow. |

### TC-REQ-4 Trash and restore a requirement
type: Test case
verifies: FR-REQ-5
automated: yes
result: passed

Preconditions: A requirement with one link and one revision.

| Action | Expected |
|---|---|
| Move the requirement to the trash. | It no longer appears in the specification or the search. |
| Restore it from the project's trash. | It appears at its old place with its link. |
