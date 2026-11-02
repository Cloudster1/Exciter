# Test specification / Pages

Spaces, the page tree, drafts, history and writing.

### TC-SPC-1 Make a space and open its home page
type: Test case
verifies: FR-SPC-1
automated: yes
result: passed

Preconditions: A person who may create spaces.

| Action | Expected |
|---|---|
| Make a space with the key DOC and the name Docs. | The space opens on its home page and appears in the list of spaces. |
| Rename the space in its settings. | The list of spaces and the space's heading show the new name. |

### TC-SPC-2 Arrange, move and copy pages
type: Test case
verifies: FR-SPC-2
automated: yes
result: passed

Preconditions: A space with a page A and a page B, and a person who may add pages.

| Action | Expected |
|---|---|
| Add a folder F and a page C below A. | The tree shows C below A and F beside them. |
| Move C below B, then copy B with its pages. | C stands below B in the tree and the copy of B has a copy of C below it. |

### TC-SPC-3 Delete a page and restore it
type: Test case
verifies: FR-SPC-3
automated: yes
result: passed

Preconditions: A page with a child page, and a person who may delete it.

| Action | Expected |
|---|---|
| Delete the page. | The page and its child leave the tree and show in the trash. |
| Restore the page from the trash. | The page and its child are back in their places. |

### TC-DRF-1 Readers see the published page while a draft is edited
type: Test case
verifies: FR-DRF-1
automated: yes
result: passed

Preconditions: A published page, an author who may edit it and a reader who may only read it.

| Action | Expected |
|---|---|
| As the author, change the page's text without publishing. | The author sees the draft and the page says the author has one. |
| As the reader, open the page. | The reader sees the page as it was published. |

### TC-DRF-2 Publish with a note
type: Test case
verifies: FR-DRF-2
automated: yes
result: passed

Preconditions: A published page, its watcher and an author with a draft.

| Action | Expected |
|---|---|
| Publish the draft with the note Fixed the dates and the box to tell the watchers ticked. | The page shows the new text as its newest version. |
| Open the history and the notifications of the watcher. | The history shows the note, and the watcher was told of the change. |

### TC-DRF-3 A concurrent publish is not overwritten
type: Test case
verifies: FR-DRF-3
automated: yes
result: passed

Preconditions: A published page and two authors, each with a draft begun on the same version.

| Action | Expected |
|---|---|
| As the first author, publish the draft. | The page shows the first author's text. |
| As the second author, publish the draft. | Nothing is published; Stator offers to compare the draft or to publish it anyway. |

### TC-DRF-4 Compare two versions
type: Test case
verifies: FR-DRF-4
automated: no
result: passed

Preconditions: A page with two published versions.

| Action | Expected |
|---|---|
| Open the page's history and pick both versions. | What the later version added and removed is marked. |

### TC-DRF-5 Restore an earlier version
type: Test case
verifies: FR-DRF-5
automated: yes
result: passed

Preconditions: A page with three published versions.

| Action | Expected |
|---|---|
| Restore version 1. | The page shows the text of version 1 as version 4. |
| Open the history. | All four versions are listed. |

### TC-DRF-6 Live pages follow typing
type: Test case
verifies: FR-DRF-7
automated: no
result: passed

Preconditions: A page made live, an editor and a reader.

| Action | Expected |
|---|---|
| As the editor, type a sentence. | The page carries a Live badge and the editor's button reads Done. |
| As the reader, keep the page open. | The sentence shows within a few seconds, with no publish. |
| As the editor, look at the audit log. | It records that the page was made live. |

### TC-DRF-7 A review publishes with enough approvals
type: Test case
verifies: FR-DRF-8
automated: yes
result: passed

Preconditions: A page held by a rule of two approvals, its author and two named reviewers.

| Action | Expected |
|---|---|
| As the author, try to publish the draft. | Stator refuses and offers to submit it for review. |
| Submit it. As the first reviewer, approve. | The page still shows its old text. |
| As the second reviewer, approve. | The draft is the newest version and the history names both approvals. |

### TC-DRF-8 Readers confirm a page
type: Test case
verifies: FR-DRF-9
automated: yes
result: passed

Preconditions: A page whose editor asked for confirmation, and two readers.

| Action | Expected |
|---|---|
| As the first reader, confirm the page. | The editor sees the first reader as confirmed and the second as not. |
| As an editor, publish a new version. | Both readers are asked to confirm again. |

### TC-EDT-1 Add the blocks of the editor
type: Test case
verifies: FR-EDT-1
automated: no
result: passed

Preconditions: An editor with a new page.

| Action | Expected |
|---|---|
| Add a heading, a list, a table, a code block, a panel, an expand block and a table of contents from the slash menu. | Each shows in the page as it will to readers after publishing. |

### TC-EDT-2 Two people write one draft
type: Test case
verifies: FR-EDT-2
automated: yes
result: passed

Preconditions: A page and two people who may edit it.

| Action | Expected |
|---|---|
| Both open the editor and each types a sentence. | Both sentences show for both people, and each sees the other's caret and avatar. |
| One publishes. | The published page holds both sentences. |

### TC-TPL-1 Make a page from a template
type: Test case
verifies: FR-TPL-1
automated: yes
result: passed

Preconditions: A space and a person who may add pages.

| Action | Expected |
|---|---|
| Make a page from the template Meeting notes. | The page opens with the template's content and its grey hints. |
| Write over one hint and publish. | The hints that are left are gone from the published page. |

### TC-TPL-2 Keep templates for the organization and for a space
type: Test case
verifies: FR-TPL-2
automated: yes
result: failed
note: The variable was left empty in the page's title when the template was made from the new page button.

Preconditions: An administrator of the organization, an administrator of the space Docs and a template that asks for the variable customer.

| Action | Expected |
|---|---|
| As the organization's administrator, make the template. | Every space offers it. |
| As the administrator of Docs, make a template of its own. | Only Docs offers it. |
| Make a page from the template with the variable customer set to Acme. | The page holds Acme where the variable stood. |

### TC-ATT-1 Attach and preview a file
type: Test case
verifies: FR-ATT-1
automated: yes
result: blocked
note: No converter for Office previews runs in the test stack; the PNG half passed.

Preconditions: A page, an editor and a PNG file and a Word file.

| Action | Expected |
|---|---|
| Attach the PNG file and put it in the text. | The image shows in the page. |
| Attach the Word file and open its preview. | The preview shows the document's pages. |
