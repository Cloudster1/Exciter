# Interfaces

What Rotor offers the other tools and assistants, and what it uses of them.

### IF-1 Workflows API
type: Interface
priority: must
verification: Test
component: API
refines: SN-2
issues: ROT-13, ROT-15
guide: workflows

Rotor shall offer the capability `workflows@1` in its REST API: to list, create, read, change, delete, compare, share and
validate workflows and to read their versions.

### IF-2 Runs API with a live stream
type: Interface
priority: must
verification: Test
component: API
refines: SN-1
issues: ROT-13, ROT-22, ROT-24
guide: running

Rotor shall offer the capability `runs@1` in its REST API: to start a workflow as the caller, to list, read and cancel
runs and to stream a run's events as server-sent events.

### IF-3 Webhooks from the tools
type: Interface
priority: must
verification: Test
component: API
refines: SN-5
issues: ROT-33, ROT-35
guide: triggers

Rotor shall offer the capability `hooks@1`: to receive the signed webhook deliveries of Armature, Exciter and Stator at
`POST /hooks/{toolID}`, to answer 202, 200, 401, 404, 413 or 422 as its documentation says, and to keep the secret of
each tool for administrators only.

### IF-4 MCP endpoint for assistants
type: Interface
priority: should
verification: Test
component: API
refines: SN-6
issues: ROT-47, ROT-114
guide: assistants

Rotor shall offer the capability `mcp@1` at `POST /mcp`, behind its own OAuth authorization server, with the tools to
read and start workflows and runs and to read approvals, and a 401 that names the protected resource metadata.

### IF-5 Service account for wiring the suite
type: Interface
priority: could
verification: Test
component: API
refines: SN-7
issues: ROT-168, ROT-181
guide: administration

Rotor shall take the access token of a service account of the realm for four routes, to list and add tools and to read
and set their webhook secrets, and shall show each tool's hook address at the place tools deliver to.

### IF-6 Sign-in through Keycloak
type: Interface
priority: must
verification: Test
component: Sign-in
refines: SN-4
issues: ROT-6, ROT-53, ROT-172
guide: getting-started

Rotor shall sign people in through the realm `dynamo` of Keycloak that Exciter, Armature and Stator share, and shall end
all of a person's sessions when Keycloak signs them out everywhere.

### IF-7 MCP of the tools, as the person
type: Interface
priority: must
verification: Test
component: Connections
refines: SN-4
issues: ROT-25, ROT-45, ROT-139
guide: connections

Rotor shall call the MCP endpoint of Exciter and Armature with a token for the person who started the run, so that
Exciter requirements, Armature issues and comments are written as that person.

### IF-8 Language model providers
type: Interface
priority: must
verification: Test
component: Models
refines: SN-7
issues: ROT-20
guide: administration

Rotor shall call the model providers an administrator added, with the key from OpenBao handed to the run's sandbox over
its channel, never in a file or the environment.
