# Test specification / Interfaces

### TC-IF-1 A remote link shows on its issue
type: Test case
verifies: IF-1
automated: yes
result: passed

Preconditions: An issue the tester may edit, and a token that may write it.

| Action | Expected |
|---|---|
| Add a remote link to the issue through the REST API, with relation `relation`. | The API answers 201 with the link. |
| Open the issue. | The link shows under its relation with its title. |

Parameters: Parameters: relation = implements, verifies

### TC-IF-2 The meta answer lists capabilities
type: Test case
verifies: IF-2
automated: yes
result: passed

Preconditions: No sign-in.

| Action | Expected |
|---|---|
| Call GET /api/v1/meta. | It answers 200 with the build and the API version. |
| Read the capabilities. | They are sorted and include remote-links@1 and webhooks@2. |

### TC-IF-3 A version 2 endpoint is posted a CloudEvent
type: Test case
verifies: IF-3
automated: yes
result: passed

Preconditions: Two endpoints, one registered for version 1 and one for version 2.

| Action | Expected |
|---|---|
| Create an issue. | Version 1 gets the envelope with version 1; version 2 gets a CloudEvent with type dev.armature.issue.created. |
| Transition the issue. | The payload names the status categories. |

### TC-IF-4 A session of the realm signs in silently
type: Test case
verifies: IF-4
automated: no
result: passed

Preconditions: The suite stack, and a tester signed in to Exciter.

| Action | Expected |
|---|---|
| Open Armature in the same browser. | Armature opens the page asked for, signed in, without a click. |
| Sign out of Armature. | The realm's session ends too. |

### TC-IF-5 A connected app gets a narrower token
type: Test case
verifies: IF-5
automated: yes
result: passed

Preconditions: A connected app registered by an administrator, and a person who consents to part of its scopes.

| Action | Expected |
|---|---|
| Run the consent flow and approve one scope. | The app receives an access token with that scope. |
| Use the token for an action outside the scope. | The API refuses it. |

### TC-IF-6 A model works through MCP and the proxy keeps the token
type: Test case
verifies: IF-6
automated: no
result: blocked
note: The test Gitea does not start on the runner since its image was updated; to be run again when it does.

Preconditions: An MCP client and a repository on the test Gitea.

| Action | Expected |
|---|---|
| List the tools at the MCP endpoint. | The marked operations are listed. |
| Clone through the git proxy. | The clone works, and no host token appears in the answers. |

### TC-IF-7 A partner reads an issue and asks for access
type: Test case
verifies: IF-7
automated: yes
result: passed

Preconditions: A person with a token and an issue they may not see.

| Action | Expected |
|---|---|
| Read the issue by key. | The API answers 404 not found. |
| Post an access request for the key. | The API answers the same for a key that does not exist. |

### TC-IF-8 The documentation link shows in the sidebar
type: Test case
verifies: IF-8
automated: no
result: passed

Preconditions: A project with a documentation link set.

| Action | Expected |
|---|---|
| Open the project. | The sidebar shows the documentation link. |
| Choose it. | The linked page opens. |
