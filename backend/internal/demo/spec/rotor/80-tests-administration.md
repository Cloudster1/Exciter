# Test specification / Administration

### TC-ADM-1 An administrator adds a model provider
type: Test case
verifies: FR-ADM-1
automated: yes
result: passed

Preconditions: An administrator and an OpenAI-compatible server.

| Action | Expected |
|---|---|
| Add the provider with its key and test the connection. | The test succeeds and the models list. |
| Price a model and pick it as the default. | A run without a model of its own uses it. |
| Look for the key outside OpenBao. | It is not in the database or the environment. |

### TC-ADM-2 A person's daily limit stops a run
type: Test case
verifies: FR-ADM-2
automated: yes
result: passed

Preconditions: A daily limit of `limit` for the tester.

| Action | Expected |
|---|---|
| Start runs until the limit is passed. | The run that would pass it stops and says which limit. |
| Open the usage page. | It shows what the tester used. |

Parameters: limit = tokens, cost, concurrent runs

### TC-ADM-3 The audit log is filtered, exported and unchangeable
type: Test case
verifies: FR-ADM-3
automated: yes
result: passed

Preconditions: An administrator and a log holding sign-ins, runs and approvals.

| Action | Expected |
|---|---|
| Filter by person and dates. | Only the matching entries show. |
| Export as `format`. | The file holds what is shown. |
| Look at the log. | The export is itself an entry. |
| Try to update or delete an entry as the database role of the api. | The database refuses it. |

Parameters: format = CSV, JSON

### TC-ADM-4 A service account adds a tool
type: Test case
verifies: FR-ADM-4
automated: yes
result: passed

Preconditions: A token of the client `dynamo-connect` with the administrator group, and one without it.

| Action | Expected |
|---|---|
| Add a tool with the first token. | The tool is added and the audit log names the service account. |
| Read a workflow with the first token. | The route refuses it and names the four it accepts. |
| Add a tool with the second token. | The refusal names the missing group. |
