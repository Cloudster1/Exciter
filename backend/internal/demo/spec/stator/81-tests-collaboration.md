# Test specification / Collaboration and access

Comments, notifications, search, labels and permissions.

### TC-COM-1 Comment on a page and on a passage
type: Test case
verifies: FR-COM-1
automated: yes
result: passed

Preconditions: A published page and two people who may comment.

| Action | Expected |
|---|---|
| As the first, comment on the page and on a selected passage. | Both show with their author, the second at its passage. |
| As the second, reply to the passage comment and resolve it, then reopen it. | The reply shows under the comment, and the thread is resolved and then open again. |

### TC-COM-2 Only the author and a space administrator delete a comment
type: Test case
verifies: FR-COM-2
automated: yes
result: passed

Preconditions: A comment by person A, and persons B and an administrator of the space.

| Action | Expected |
|---|---|
| As B, try to delete A's comment. | Stator refuses. |
| As the administrator, delete it. | The comment is gone. |

### TC-COM-3 Mention a person
type: Test case
verifies: FR-COM-3
automated: yes
result: passed

Preconditions: A published page, an author, a person who may view it and one who may not.

| Action | Expected |
|---|---|
| Mention the first person in a comment. | The first person is told. |
| Try to mention the second. | The second is not offered and is not told. |

### TC-COM-4 Watch a page
type: Test case
verifies: FR-COM-5
automated: yes
result: passed

Preconditions: A published page and a person who watches it.

| Action | Expected |
|---|---|
| Publish a change, move the page and comment on it. | The watcher finds three notifications in the application. |
| Look at the watcher's mailbox. | Each change was also mailed. |

### TC-COM-5 A notification never names a page its reader may not view
type: Test case
verifies: FR-COM-6
automated: yes
result: passed

Preconditions: A page restricted to its author, and a person who watches the space.

| Action | Expected |
|---|---|
| As the author, publish a change to the restricted page. | The watcher gets no notification in the application and no email. |

### TC-SRC-1 Search with filters
type: Test case
verifies: FR-SRC-1
automated: yes
result: passed

Preconditions: Pages in two spaces, one holding the phrase launch plan in its text and a comment.

| Action | Expected |
|---|---|
| Search for the phrase launch plan in quotes. | The page and the comment are listed. |
| Search with the word -plan, then narrow to the second space. | Pages with plan are left out, and only the second space's pages remain. |

### TC-SRC-2 Search leaves out what the searcher may not read
type: Test case
verifies: FR-SRC-2
automated: yes
result: passed

Preconditions: A page with the word zebra restricted to its author, a page with the word zebra open to all, and a reader.

| Action | Expected |
|---|---|
| As the reader, search for zebra. | Only the open page is listed. |
| As the reader, search for zebre. | The open page is listed, the restricted one is not. |

### TC-SRC-3 Label a page and list the pages of the label
type: Test case
verifies: FR-SRC-3, FR-SRC-4
automated: yes
result: passed

Preconditions: Two published pages and a draft page, an editor.

| Action | Expected |
|---|---|
| Add the label plan to the published pages and the draft page. | Each page shows the label, and plan is offered the next time a label is added. |
| Open the label plan as a reader. | The two published pages are listed and the draft page is not. |
| Take the label off one page. | The label lists one page. |

### TC-ACC-1 Give access to a space by grid
type: Test case
verifies: FR-ACC-1
automated: yes
result: passed

Preconditions: A space, an administrator, a group and a person in it.

| Action | Expected |
|---|---|
| Give the group View and Add comments. | The person may read and comment and may not add pages. |
| Remove the last administrator. | Stator refuses. |

### TC-ACC-2 Restrict a page and its subpages
type: Test case
verifies: FR-ACC-2
automated: yes
result: passed

Preconditions: A page with a child page, an editor and a reader.

| Action | Expected |
|---|---|
| Restrict viewing of the page to the editor. | The reader finds neither the page nor the child. |
| Restrict the page so that the editor could not view it any more. | Stator refuses the change. |

### TC-ACC-3 Check a person's access
type: Test case
verifies: FR-ACC-3
automated: no
result: passed

Preconditions: A restricted page and an administrator of the space.

| Action | Expected |
|---|---|
| Check the access of a person who may not edit the page. | The dialog says view yes, edit no, and names the restriction that decides it. |

### TC-ACC-4 Invite a guest
type: Test case
verifies: FR-ACC-5
automated: yes
result: passed

Preconditions: Two spaces, an administrator of the organization and a person outside it.

| Action | Expected |
|---|---|
| Invite the person to the first space with Read and comment. | The invitation shows in the audit log. |
| Sign in as the guest and open the second space and the list of people. | Neither is reachable; the first space may be read and commented on but not edited. |

### TC-ACC-5 Open a page to the public
type: Test case
verifies: FR-ACC-6
automated: yes
result: passed

Preconditions: The organization allows public links and a published page of a space.

| Action | Expected |
|---|---|
| Make a public link to the page and open it without signing in. | The page shows alone, without comments, history and names. |
| Revoke the link and open it again. | The page is no longer shown. |
