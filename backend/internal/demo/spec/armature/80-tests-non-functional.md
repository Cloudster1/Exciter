# Test specification / Non-functional requirements

### TC-NFR-1 Operations answer within their time
type: Test case
verifies: NFR-1
automated: yes
result: failed
note: Opening the plan of a project with 10,000 issues took 3.8 seconds at the 95th percentile, over the 3 seconds.

Preconditions: A development stack with 10,000 issues, and 50 requests per operation.

| Action | Expected |
|---|---|
| Measure `operation`, 50 times, one after the other. | The 95th percentile is within the time in the table. |

Parameters: Parameters: operation = open an issue, search by NQL, open a board, open the plan

### TC-NFR-2 A projects list of 5,000 projects opens in time
type: Test case
verifies: NFR-2
automated: yes
result: passed

Preconditions: An installation with 5,000 projects.

| Action | Expected |
|---|---|
| Open the projects list 50 times. | The 95th percentile is under 2 seconds. |
| Type a letter in a label field. | Matches are offered without loading every label. |

### TC-NFR-3 Contrast, accessibility check and phone width
type: Test case
verifies: NFR-3
automated: yes
result: passed

Preconditions: The browser suite and a viewport 360 pixels wide.

| Action | Expected |
|---|---|
| Run the accessibility check on every page of the suite. | No violation is reported. |
| Open `page` at 360 pixels. | The page does not scroll sideways. |

Parameters: Parameters: page = the board, the issue, the plan, the settings

### TC-NFR-4 Every page reads in both languages
type: Test case
verifies: NFR-4
automated: no
result: passed

Preconditions: A user with the profile language `language`.

| Action | Expected |
|---|---|
| Open the board, an issue and the settings. | Each label, message and time reads in the language. |

Parameters: Parameters: language = English, German

### TC-NFR-5 A first delivery arrives within 10 seconds
type: Test case
verifies: NFR-5
automated: yes
result: passed

Preconditions: An endpoint that records the time of each delivery.

| Action | Expected |
|---|---|
| Create an issue and note the time. | A delivery arrives within 10 seconds. |
| Make the endpoint answer after 12 seconds. | The attempt is logged as failed. |

### TC-NFR-6 A delivery carries both signatures
type: Test case
verifies: NFR-6
automated: yes
result: passed

Preconditions: An endpoint with a secret.

| Action | Expected |
|---|---|
| Receive a delivery. | The headers hold both signatures and the timestamp. |
| Compute the two signatures from the body and the timestamp. | Both equal the headers. |
