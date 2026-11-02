# Test specification / Import and export

### TC-IMP-1 Import a CSV file
type: Test case
verifies: FR-IMP-1
automated: yes
result: passed

Preconditions: A CSV file of three rows, one with a bad status.

| Action | Expected |
|---|---|
| Upload the file and open the mapping step. | Columns are mapped from their headers and the bad row is flagged. |
| Run the import. | Two requirements are made and the bad row is reported. |

### TC-IMP-2 A second ReqIF exchange updates
type: Test case
verifies: FR-IMP-2
automated: yes
result: passed

Preconditions: A ReqIF file of four requirements.

| Action | Expected |
|---|---|
| Import the file. | Four requirements, with their types and relations, are made. |
| Export the project, change one statement and import the file again. | The project still holds four requirements, with one updated. |

### TC-IMP-3 Import a Word document twice
type: Test case
verifies: FR-IMP-3
automated: yes
result: blocked
note: The sample Word document was not on the test stack; the run waits for it.

Preconditions: A Word document with two headings and four paragraphs.

| Action | Expected |
|---|---|
| Import the document. | Two sections and four requirements are made. |
| Change a paragraph and import again. | The requirement is updated and none is added. |
