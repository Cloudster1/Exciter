# Test specification / Non-functional

### TC-NFR-1 Coverage of a large project
type: Test case
verifies: NFR-1
automated: yes
result: blocked
note: The load fixture of 5,000 requirements was not available on the test stack.

Preconditions: A project of 5,000 requirements on the development stack.

| Action | Expected |
|---|---|
| Ask the project's coverage 100 times. | The 95th percentile of the answers is within 2 seconds. |

### TC-NFR-2 A large specification opens by windows
type: Test case
verifies: NFR-2
automated: yes
result: passed

Preconditions: A project of `size` requirements.

Parameters: size = 1000, 5000; within = 0.5 s, 1 s

| Action | Expected |
|---|---|
| Ask the first window of 100 entries 50 times. | The 95th percentile is within `within`. |
| Ask the whole specification as a document. | It is answered within the request limit. |

### TC-NFR-3 A hidden project answers not found
type: Test case
verifies: NFR-3
automated: yes
result: passed

Preconditions: A project that bob may not view.

| Action | Expected |
|---|---|
| Bob reads the project, a requirement of it and a baseline of it. | Each answers 404. |
| Count the project's requirements on a dashboard Bob may view. | The figure leaves them out. |

### TC-NFR-4 The pages pass the accessibility run
type: Test case
verifies: NFR-4
automated: yes
result: passed

Preconditions: The browser suite on the development stack.

| Action | Expected |
|---|---|
| Run the accessibility specs in the light theme and then the dark theme. | No serious or critical violation is reported. |

### TC-NFR-5 The catalogues match
type: Test case
verifies: NFR-5
automated: yes
result: passed

Preconditions: The web client's source.

| Action | Expected |
|---|---|
| Compare the German catalogue with the English one. | No string of the English catalogue is missing in German. |
| Switch to German and export a specification. | The export is in German. |

### TC-NFR-6 Limits are refused with a sentence
type: Test case
verifies: NFR-6
automated: yes
result: passed

Preconditions: A project with a test case type.

| Action | Expected |
|---|---|
| Attach a file of 51 MB. | It is refused with the limit named. |
| Save a test case of 101 steps. | It is refused with the limit named. |
| Add a 501st link to one requirement. | It is refused with the limit named. |

### TC-NFR-7 A delivery is retried and an endpoint ends
type: Test case
verifies: NFR-7
automated: yes
result: passed

Preconditions: An endpoint that fails every post.

| Action | Expected |
|---|---|
| Change a requirement and wait for three attempts. | The waits between them grow. |
| Let the endpoint fail for 24 hours. | It is turned off. |

### TC-NFR-8 Tokens are not stored in the clear
type: Test case
verifies: NFR-8
automated: yes
result: passed

Preconditions: A personal access token and an Armature token stored.

| Action | Expected |
|---|---|
| Read the token tables in the database as the administrator role. | The personal token is a digest and the Armature token is sealed. |
| Save a connection to an address inside the network that is not allowed. | It is refused. |
