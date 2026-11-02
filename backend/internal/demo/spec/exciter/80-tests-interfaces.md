# Test specification / Interfaces

### TC-IF-1 The meta document lists the capabilities
type: Test case
verifies: IF-1, IF-2, IF-3, IF-5, IF-6, IF-12
automated: yes
result: passed

Preconditions: The development stack.

| Action | Expected |
|---|---|
| Read `GET /api/v1/meta` without signing in. | It reports the build and lists `requirement-list@1`, `trace-links@1`, `webhooks@2`, `mcp@1` and `theme-export@1`. |
| Read `GET /api/v1/projects` with a token. | The projects the token's person may read are listed. |

### TC-IF-2 A partner lists requirements by a query
type: Test case
verifies: IF-2, IF-11
automated: yes
result: passed

Preconditions: A project with ten requirements in two statuses.

| Action | Expected |
|---|---|
| List the project's requirements with the query `status = Approved` and a limit of 3. | Three of the matching requirements are listed with the total. |
| Send a query that does not read. | The answer is `bad_query` with a position. |

### TC-IF-3 A partner trades a token
type: Test case
verifies: IF-4
automated: yes
result: passed

Preconditions: An app registered with the partner grant, and a token of the realm for alice.

| Action | Expected |
|---|---|
| Exchange the token at `POST /oauth/token`. | An access and a refresh token of alice are answered. |
| Exchange a token of the app's own grant for a narrower one limited to reading. | A token that refuses writes is answered. |

### TC-IF-4 A webhook endpoint on version 2 gets a CloudEvent
type: Test case
verifies: IF-5
automated: yes
result: passed

Preconditions: An endpoint registered for envelope version 2.

| Action | Expected |
|---|---|
| Send a test delivery. | The endpoint receives a signed CloudEvent in structured mode. |

### TC-IF-5 An assistant lists the tools of the endpoint
type: Test case
verifies: IF-6
automated: yes
result: passed

Preconditions: A token of alice.

Parameters: rev = 2025-11-25, 2025-06-18

| Action | Expected |
|---|---|
| Initialise a session in the revision `rev`. | The server answers in that revision and lists its tools. |

### TC-IF-6 Export and import ReqIF
type: Test case
verifies: IF-7
automated: no
result: passed

Preconditions: A project with a requirement and a link.

| Action | Expected |
|---|---|
| Export the project as ReqIF and open the file in another tool's viewer. | The requirement, its type and its relation are read. |

### TC-IF-7 A pipeline reports and asks the verdict
type: Test case
verifies: IF-8
automated: yes
result: passed

Preconditions: A token of the evidence scope.

| Action | Expected |
|---|---|
| Run `exciter-ci results` with a JUnit file. | The results are landed and the keys that verify nothing are printed. |
| Run `exciter-ci check --fail gaps`. | The exit code is 1 when a gap remains and 0 when none does. |

### TC-IF-8 The suite signs in once
type: Test case
verifies: IF-9, IF-10, IF-11
automated: yes
result: passed

Preconditions: The suite stack with Exciter, Armature and Stator on one realm.

| Action | Expected |
|---|---|
| Sign in to Exciter as alice. | Alice is connected to Armature and Stator without a token. |
| Switch Armature off the capability list and open a requirement's issues. | A sentence names `issue-embed@1` and says to update Armature. |
