# Test specification / Documents

### TC-DOC-1 Edit and change status in the document
type: Test case
verifies: FR-DOC-1
automated: yes
result: passed

Preconditions: A section of three requirements in the first status.

| Action | Expected |
|---|---|
| Edit the statement of the first requirement in place and save it. | It saves as a new revision and the change is marked. |
| Select all three and move them to the next status. | Each is moved by its own transition, and one refused move does not stop the others. |

### TC-DOC-2 Download the specification as Word and PDF
type: Test case
verifies: FR-DOC-2
automated: yes
result: passed

Preconditions: A project of ten requirements with links.

| Action | Expected |
|---|---|
| Download the project as Word. | The file has a cover, contents and a table of fields for each requirement. |
| Download the same project as PDF. | The PDF has the same contents. |

### TC-DOC-3 Export through a Word template and as a site
type: Test case
verifies: FR-DOC-3
automated: yes
result: passed

Preconditions: A project with a stored Word template.

| Action | Expected |
|---|---|
| Export through the template. | The file takes the template's styles. |
| Export as a static site and open it without Exciter. | Its pages open and link to each other. |
