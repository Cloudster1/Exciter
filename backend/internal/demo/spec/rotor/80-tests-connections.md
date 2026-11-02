# Test specification / Connections

### TC-CON-1 An administrator adds a tool by its address
type: Test case
verifies: FR-CON-1
automated: yes
result: passed

Preconditions: An administrator and a tool that serves MCP and OAuth metadata.

| Action | Expected |
|---|---|
| Add the tool by its address. | Settings lists it with what it offers. |
| Read Rotor's registration at the tool. | Rotor is registered as an OAuth client. |

### TC-CON-2 Connect and disconnect an account
type: Test case
verifies: FR-CON-2
automated: yes
result: passed

Preconditions: A tool Rotor has added and a tester with an account in it.

| Action | Expected |
|---|---|
| Connect the tool on the Connections page. | The tool's own consent page opens; after consent Rotor shows when it was connected. |
| Search the database for the refresh token. | It is not there; OpenBao holds it. |
| Disconnect. | The tool no longer accepts the grant. |

### TC-CON-3 A run's token is narrowed and revoked
type: Test case
verifies: FR-CON-3
automated: yes
result: passed

Preconditions: A binding with the scope `read` and a connected tester.

| Action | Expected |
|---|---|
| Start a run and read the token's scopes at the tool. | They are `read` and the token belongs to the tester. |
| Let the run end. | The tool refuses the token. |

### TC-CON-4 A run writes in Armature as the person
type: Test case
verifies: FR-CON-4
automated: yes
result: passed

Preconditions: A project in Armature where the tester may comment, and a workflow that adds a comment.

| Action | Expected |
|---|---|
| Run the workflow as the tester. | The comment is added. |
| Read its author in Armature. | It is the tester, with the app named beside. |
| Run it as a person who only reads the project. | Armature refuses the comment and the run says so. |

### TC-CON-5 A run reads Stator pages as the person
type: Test case
verifies: FR-CON-5
automated: no
result: blocked
note: Stator does not offer OAuth to Rotor yet, so no connection can be made.

Preconditions: A space in Stator the tester reads.

| Action | Expected |
|---|---|
| Run a workflow that reads a page. | The run returns the page's title. |
