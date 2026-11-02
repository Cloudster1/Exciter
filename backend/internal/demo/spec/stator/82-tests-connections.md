# Test specification / Connections, export and interfaces

The other tools, documents and the interfaces Stator offers and uses.

### TC-CON-1 Connect Armature and one's own account
type: Test case
verifies: FR-CON-1
automated: yes
result: passed

Preconditions: A running Armature, an administrator of Stator and a person with a token of Armature.

| Action | Expected |
|---|---|
| As the administrator, connect the Armature installation. | The connection shows as working. |
| As the person, paste the token in the profile. | The profile shows the account as connected. |

### TC-CON-2 A typed issue key becomes a chip
type: Test case
verifies: FR-CON-2
automated: yes
result: passed

Preconditions: A connected author, an issue the author may see and one that a reader may not.

| Action | Expected |
|---|---|
| Type the key of the first issue and a space. | A chip shows the issue's key and status. |
| As the reader, open a page holding the key of the issue the reader may not see. | The page says the issue is not visible or gone and shows no title. |

### TC-CON-3 A page tells its issue
type: Test case
verifies: FR-CON-4
automated: yes
result: passed

Preconditions: A connected author and an issue.

| Action | Expected |
|---|---|
| Publish a page that names the issue. | The issue in Armature lists a remote link to the page. |
| Take the key out of the page and publish. | The link is gone from the issue. |

### TC-CON-4 A requirement of Exciter shows as a chip
type: Test case
verifies: FR-CON-5
automated: no
result: blocked
note: The Exciter test instance could not be reached from the test environment on the day.

Preconditions: A connected Exciter and an author whose Exciter account is connected.

| Action | Expected |
|---|---|
| Type the key of a requirement and a space. | A chip shows the key and status of the requirement. |
| Add a requirement block with the same key. | The block shows the statement and the rationale. |

### TC-CON-5 A tool puts a link on a page
type: Test case
verifies: FR-CON-6
automated: yes
result: passed

Preconditions: A page, an editor's token and a token that may only read.

| Action | Expected |
|---|---|
| Put a remote link with the source Exciter on the page through the API. | The API answers 201 and the page lists the link under Exciter. |
| Put the same link again with another title. | The API answers 200 and the page shows the new title once. |
| Put a link with the read-only token. | The API answers 403. |

### TC-EXP-1 Export a page as PDF or Word
type: Test case
verifies: FR-EXP-1
automated: yes
result: failed
note: The Word file lost the shading of the table header; the PDF passed.

Preconditions: A page with a table, and a reader.

| Action | Expected |
|---|---|
| Export the page as a format. | A file of that format downloads. |
| Open the file. | It shows the page's text and table, the organization's logo and the footer line. |

Parameters: format = PDF, Word

### TC-EXP-2 Markdown in and out
type: Test case
verifies: FR-EXP-2
automated: yes
result: passed

Preconditions: A page with headings and a list, and a Markdown file with an HTML image on a line of its own.

| Action | Expected |
|---|---|
| Export the page as Markdown and import the file as a new page. | The new page has the same headings and list. |
| Import the file with the HTML image. | Stator refuses it and says which line it cannot take. |

### TC-API-1 Make, limit and revoke a token
type: Test case
verifies: FR-API-1
automated: yes
result: passed

Preconditions: A person and an administrator.

| Action | Expected |
|---|---|
| Make a token limited to reading and to one space. | The secret shows once and is not shown again. |
| Revoke it as the administrator and call the API with it. | The call is refused with 401. |

### TC-API-2 A token does no more than its owner
type: Test case
verifies: FR-API-2
automated: yes
result: passed

Preconditions: A read-only token, a token of a person who may not view a page, and an app with a grant.

| Action | Expected |
|---|---|
| Write a page with the read-only token. | Stator answers 403 read_only_token. |
| Read the page the other token's owner may not view. | Stator answers 404. |
| Ask for a narrower token for one run. | The token reaches no more than the app's grant. |

### TC-API-3 Use the wiki from an assistant
type: Test case
verifies: FR-API-3
automated: yes
result: passed

Preconditions: An assistant connected by OAuth with a read-only grant.

| Action | Expected |
|---|---|
| List the tools. | Only reading tools are offered. |
| Call a writing tool anyway. | The call is refused with the read-only sentence. |

### TC-API-4 Deliver a webhook
type: Test case
verifies: FR-API-4
automated: yes
result: passed

Preconditions: An endpoint subscribed to page.published whose first attempt fails.

| Action | Expected |
|---|---|
| Publish a page. | The endpoint is called with a signed delivery and the log shows a failed attempt. |
| Wait for the retry. | The delivery arrives with the same id and the log shows both attempts. |
