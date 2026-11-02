# Test specification / Quality and interfaces

The criteria and the interfaces of the specification.

### TC-NFR-1 Reads and the tree under the standard load
type: Test case
verifies: NFR-PERF-1
automated: yes
result: passed

Preconditions: A stack with a space of 2,000 pages and the k6 scripts.

| Action | Expected |
|---|---|
| Run the standard profile. | The run ends without a broken threshold. |
| Read the 95th percentiles of the reads and the whole tree. | Reads are under 300 ms and the whole tree under 1 s. |

### TC-NFR-2 Search under the standard load
type: Test case
verifies: NFR-PERF-2
automated: yes
result: failed
note: The 95th percentile was 912 ms on the shared CI runner; it was 458 ms on the reference machine.

Preconditions: A stack with a space of 2,000 pages and the k6 scripts.

| Action | Expected |
|---|---|
| Run the standard profile. | The run ends without a broken threshold. |
| Read the 95th percentile of the searches. | It is under 800 ms. |

### TC-NFR-3 Writing together under the standard load
type: Test case
verifies: NFR-PERF-3
automated: yes
result: passed

Preconditions: A stack with a space of 2,000 pages and the k6 scripts.

| Action | Expected |
|---|---|
| Run the standard profile. | The run ends without a broken threshold. |
| Read the 95th percentiles of delivery, opening a shared draft and writes. | Delivery is under 300 ms, opening under 500 ms and writes under 1 s. |

### TC-NFR-4 Failed requests under the standard load
type: Test case
verifies: NFR-REL-1
automated: yes
result: passed

Preconditions: A stack with a space of 2,000 pages and the k6 scripts.

| Action | Expected |
|---|---|
| Run the standard profile. | No scenario has 1 % or more failed requests, the 409 answers not counted. |

### TC-NFR-5 Pages pass the accessibility checks
type: Test case
verifies: NFR-ACC-1
automated: yes
result: passed

Preconditions: The browser tests with axe.

| Action | Expected |
|---|---|
| Run the accessibility checks on each page in the theme. | No violation of level AA is found. |
| Measure the contrast of subtle text and the danger button. | Each reaches 4.5 to 1. |

Parameters: theme = light, dark

### TC-NFR-6 Switch the language
type: Test case
verifies: NFR-I18N-1
automated: no
result: passed

Preconditions: A person signed in.

| Action | Expected |
|---|---|
| Choose German in the profile and open a space. | The interface is German. |
| Sign out and in again. | The interface is still German. |

### TC-NFR-7 The database refuses what the service would
type: Test case
verifies: NFR-SEC-1
automated: yes
result: passed

Preconditions: The integration tests with a database user bound by row level security.

| Action | Expected |
|---|---|
| Read the comment of a page the user may not view straight from the database. | No row is returned. |
| Write a token of another person. | The database refuses. |

### TC-NFR-8 Backup and restore
type: Test case
verifies: NFR-OPS-1
automated: yes
result: passed

Preconditions: A running installation with a space.

| Action | Expected |
|---|---|
| Take a backup. | One archive holds the database and the files. |
| Restore it without -yes. | Stator refuses before it opens storage. |
| Restore it with -yes onto an empty installation. | The space and its files are there. |

### TC-IF-1 The API holds to its contract
type: Test case
verifies: IF-1
automated: yes
result: passed

Preconditions: The integration tests and api/openapi.json.

| Action | Expected |
|---|---|
| Call every operation of the contract with and without rights. | Each answers the status and the sentence the contract names. |

### TC-IF-2 Pages of a label through the API
type: Test case
verifies: IF-2
automated: yes
result: passed

Preconditions: A label on a published page, a draft page and a page the caller may not read.

| Action | Expected |
|---|---|
| Call GET /labels/plan/pages. | Only the published page the caller may read is listed. |

### TC-IF-3 Remote links through the API
type: Test case
verifies: IF-3
automated: yes
result: passed

Preconditions: A page with a remote link and a webhook subscribed to page.remote_link.removed.

| Action | Expected |
|---|---|
| Delete the link with DELETE. | The API answers 204 and the webhook is called with the link. |
| Delete a link that is not on the page. | The API answers 404. |

### TC-IF-4 The envelope of a delivery
type: Test case
verifies: IF-4
automated: yes
result: passed

Preconditions: An endpoint and its secret.

| Action | Expected |
|---|---|
| Send a test delivery. | The envelope has a version, an id, a topic, a time and a payload. |
| Check the signature with the secret and the time of the attempt. | It is valid, and the same event redelivered has the same id. |

### TC-IF-5 Connect an app by OAuth
type: Test case
verifies: IF-5
automated: yes
result: passed

Preconditions: An app that registers itself and an administrator.

| Action | Expected |
|---|---|
| Register the app and connect it by authorization code with PKCE. | The app is pending until the administrator allows it, then gets a token. |
| Use the token at the REST API and at the MCP endpoint. | The REST API refuses it and the MCP endpoint accepts it. |

### TC-IF-6 Ask for the build and the capabilities
type: Test case
verifies: IF-6
automated: yes
result: passed

Preconditions: A running Stator and a caller with no account.

| Action | Expected |
|---|---|
| Call GET /api/v1/meta. | The answer names the product stator, the build and the sorted capabilities, such as page-remote-links@1. |

### TC-IF-7 Armature's deliveries
type: Test case
verifies: IF-7
automated: yes
result: passed

Preconditions: A connected Armature, the signing secret and a cached issue.

| Action | Expected |
|---|---|
| Send a delivery of version 2. | Stator answers 422. |
| Send a delivery signed ten minutes ago. | Stator answers 401. |
| Send a delivery of issue.updated that is valid. | The chip of the issue shows the new status. |

### TC-IF-8 Exciter's requirements
type: Test case
verifies: IF-8
automated: yes
result: passed

Preconditions: A connected Exciter and a cached requirement.

| Action | Expected |
|---|---|
| Send a signed delivery that names the requirement. | The next reading of the requirement asks Exciter again. |
| Read the requirement as a person who may not see it in Exciter. | Stator reports it as not visible or gone. |

### TC-IF-9 Sign in through Keycloak
type: Test case
verifies: IF-9
automated: yes
result: passed

Preconditions: The suite's Keycloak with the realm dynamo and a user in a mapped group.

| Action | Expected |
|---|---|
| Sign in to Stator. | The person is signed in with the role the group maps to. |
| Open Armature in the same browser. | The person is signed in there without asking. |
