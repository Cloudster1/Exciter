# Test specification / Search and reports

### TC-SRC-1 Queries on text, negation and custom fields answer
type: Test case
verifies: FR-SRC-1
automated: yes
result: passed

Preconditions: A project with issues, some without a description, and a number field Effort.

| Action | Expected |
|---|---|
| Search for `query`. | The search answers the matching issues and no error. |
| Search for `description !~ foo`. | The result includes the issues that have no description. |

Parameters: Parameters: query = text ~ meta, Effort > 5, Effort = 3

### TC-SRC-2 A search is paged past 100
type: Test case
verifies: FR-SRC-2
automated: yes
result: passed

Preconditions: A project with 250 issues.

| Action | Expected |
|---|---|
| Search for all issues of the project. | The first page shows 100 results and a way to the next page. |
| Go to the third page. | It shows the last 50 issues. |

### TC-SRC-3 An export says it was cut
type: Test case
verifies: FR-SRC-3
automated: yes
result: passed

Preconditions: A project with 5,200 issues.

| Action | Expected |
|---|---|
| Export all issues of the project. | The CSV file holds 5,000 issues. |
| Read the message beside the download. | It says that the list was cut at 5,000. |

### TC-SRC-4 A shared dashboard works for a visitor
type: Test case
verifies: FR-SRC-4
automated: yes
result: passed

Preconditions: A dashboard with a sprint history widget and a share link.

| Action | Expected |
|---|---|
| Open the share link without signing in as a member. | The dashboard shows. |
| Use the Curve button of the sprint history widget. | The curve shows, with no error. |
