# Test specification / Interfaces

### TC-IF-1 The workflows API
type: Test case
verifies: IF-1
automated: yes
result: passed

Preconditions: A token of a tester.

| Action | Expected |
|---|---|
| Validate, create and read a workflow through the REST API. | Each answers with the workflow or its problems. |
| Change it, list its versions and compare two. | The versions list and the difference shows. |
| Share it with a group and delete it. | Both answer 200. |

### TC-IF-2 The runs API and its stream
type: Test case
verifies: IF-2
automated: yes
result: passed

Preconditions: A workflow the tester may run.

| Action | Expected |
|---|---|
| Start a run with `POST /workflows/{id}/runs`. | The answer holds the run. |
| Read `GET /runs/{id}/events`. | Server-sent events arrive until the run ends. |
| Cancel a second run. | It ends as cancelled. |

### TC-IF-3 The webhook answers
type: Test case
verifies: IF-3
automated: yes
result: passed

Preconditions: The secret of a tool.

| Action | Expected |
|---|---|
| Post a delivery that is `case`. | The answer is `answer`. |

Parameters: case = signed, a repeat, signed by another secret, for an unknown tool, larger than 256 KiB, of a version 3; answer = 202, 200, 401, 404, 413, 422

### TC-IF-4 The MCP endpoint
type: Test case
verifies: IF-4
automated: yes
result: passed

Preconditions: Rotor's endpoint and no token.

| Action | Expected |
|---|---|
| Post to `/mcp` without a token. | The answer is 401 and names the protected resource metadata. |
| Connect an assistant and list the tools. | `whoami`, `list_workflows`, `get_workflow`, `validate_workflow`, `start_run`, `list_runs`, `get_run`, `cancel_run`, `list_approvals` and `get_approval` are listed. |

### TC-IF-5 The service account routes
type: Test case
verifies: IF-5
automated: yes
result: passed

Preconditions: A token of `dynamo-connect`.

| Action | Expected |
|---|---|
| List the tools. | Each shows its `baseURL` and its `hookURL`. |
| Set a tool's webhook secret. | The answer is 200 and the audit log names the service account. |

### TC-IF-6 Sign-in and sign-out everywhere
type: Test case
verifies: IF-6
automated: yes
result: passed

Preconditions: A person signed in to Rotor in two browsers.

| Action | Expected |
|---|---|
| Sign out everywhere in Keycloak. | Both of Rotor's sessions end. |
| Sign in for the first time from two browsers at once. | One person exists afterwards. |

### TC-IF-7 Writes in the tools as the person
type: Test case
verifies: IF-7
automated: yes
result: passed

Preconditions: A connected tester and the tool double `tool`.

| Action | Expected |
|---|---|
| Run a workflow that writes in the tool. | The write arrives with a token of the tester. |
| Read the writer in the tool. | It is the tester. |

Parameters: tool = Exciter, Armature

### TC-IF-8 A model provider gets the key over the channel
type: Test case
verifies: IF-8
automated: yes
result: passed

Preconditions: A provider with a key in OpenBao and a run that uses it.

| Action | Expected |
|---|---|
| Inspect the sandbox's files and environment. | The key is in neither. |
| Read the model call at the provider. | It carries the key. |
