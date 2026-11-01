# Test specification / Pages

### TC-PAG-1 Readers see the published page while a draft is edited
type: Test case
verifies: FR-PAG-1
automated: yes
result: passed

Preconditions: A published page, an author who may edit it and a reader who may only read it.

| Action | Expected |
|---|---|
| As the author, change the page's text without publishing. | The author sees the draft. |
| As the reader, open the page. | The reader sees the page as it was published. |

### TC-PAG-2 Compare two versions
type: Test case
verifies: FR-PAG-2
automated: no
result: passed

Preconditions: A page with two published versions.

| Action | Expected |
|---|---|
| Open the page's history and pick both versions. | What the later version added and removed is marked. |
