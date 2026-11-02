# Functional requirements / Connections

How Rotor reaches the tools as the person.

### FR-CON-1 Add a tool by its address
type: Functional
priority: must
verification: Test
component: Connections
refines: SN-4
issues: ROT-9
guide: connections

When an administrator adds a tool by its address, Rotor shall find the tool's MCP endpoint and authorization server and
register as its OAuth client.

### FR-CON-2 Connect and disconnect an account
type: Functional
priority: must
verification: Test
component: Connections
refines: SN-4
issues: ROT-11, ROT-10
guide: connections

When a person connects a tool, Rotor shall send them to the tool's own consent page and back, shall keep the refresh
token in OpenBao and nowhere else, and on disconnecting shall revoke the grant at the tool.

Rationale: Consent given on the tool's own page is the tool's, and a secret in the database is a secret to lose.

### FR-CON-3 Hand a run a token of its own
type: Functional
priority: must
verification: Test
component: Connections
refines: SN-4
issues: ROT-12, ROT-23
guide: connections

When a run needs a tool, Rotor shall mint a token for the run's person, narrowed to the binding's scopes, hand it over to
the sandbox wrapped to be opened once, and revoke it when the run ends.

### FR-CON-4 Act in Armature
type: Functional
priority: must
verification: Test
component: Connections
refines: SN-4
issues: ROT-45
guide: connections

Where a workflow has a binding to Armature, Rotor shall read and write issues, transitions and comments with a token of
Armature's scopes for the person who started the run.

### FR-CON-5 Act in Stator
type: Functional
priority: should
verification: Test
component: Connections
refines: SN-4
issues: ROT-105
guide: connections

Where a workflow has a binding to Stator, Rotor shall read its pages with a token for the person who started the run.
