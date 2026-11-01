# Interfaces

What Stator offers the other tools and scripts of the suite, and what it uses of them.

## Offered

### IF-1 REST API described by an OpenAPI document
type: Interface
priority: must
verification: Test
component: API
refines: SN-8
issues: STA-21, STA-145, STA-170
guide: tokens-and-mcp

Stator shall offer every function of the web application as a REST API under /api/v1, described in api/openapi.json, and shall answer a request with the status and the sentence the contract names.

Rationale: Scripts and the other tools build against the contract, not against the pages.

### IF-2 Pages of a label
type: Interface
priority: should
verification: Test
component: API
refines: SN-5
issues: STA-17, STA-55
guide: labels

Stator shall answer GET /labels/{labelName}/pages with the published pages that carry the label and that the caller may read.

Rationale: Exciter and Rotor show the pages of a label, such as the guide of a tool, without copying them.

### IF-3 Remote links on pages
type: Interface
priority: must
verification: Test
component: API
refines: SN-7
issues: STA-419
guide: exciter

Stator shall keep the remote links another tool puts on a page through POST /pages/{pageID}/remote-links with a url, a title and a source, answer 201 for a new link and 200 for one with the same url, list them with GET and remove one with DELETE, and send the webhook topic page.remote_link.removed when one is taken off.

Rationale: Exciter traces requirements to pages and has to learn when a person took the trace off.

### IF-4 Webhooks with a versioned, signed envelope
type: Interface
priority: should
verification: Test
component: API
refines: SN-8
issues: STA-110, STA-412
guide: webhooks

Stator shall send each webhook delivery as an envelope with a version, an id that stays the same on every retry, a topic, the time of the event and a payload, signed together with the time of the attempt.

Rationale: A receiver written for one product of the suite serves the others.

### IF-5 MCP endpoint with OAuth for partner apps
type: Interface
priority: should
verification: Test
component: API
refines: SN-8
issues: STA-106, STA-449, STA-461
guide: tokens-and-mcp

Stator shall offer an MCP endpoint at POST /api/v1/mcp, let an app register itself and connect by OAuth 2.1 with PKCE, trade the access token of a person signed in at the identity provider for a token of theirs, and accept the access token at the MCP endpoint alone.

Rationale: Rotor and assistants act as a person without the person pasting a token.

### IF-6 Build and capabilities
type: Interface
priority: should
verification: Test
component: API
refines: SN-8
issues: STA-407, STA-409
guide: tokens-and-mcp

Stator shall answer GET /api/v1/meta, to anyone, with the product, the version of the build and the sorted names with versions of the integration features it offers.

Rationale: A partner decides from a feature name whether something is there, not from a version number.

## Used

### IF-7 Armature issues, tokens and webhooks
type: Interface
priority: must
verification: Test
component: Armature
refines: SN-7
issues: STA-27, STA-33, STA-185, STA-409
guide: armature

Stator shall read issues, projects and charts from the REST API of a connected Armature as the reading person, decide from Armature's capabilities which features it uses, and accept Armature's signed webhook deliveries of envelope version 1, refusing others with 422 and a delivery signed more than five minutes from its own clock with 401.

Rationale: Chips and blocks show current issues, and a delivery refreshes them at once.

### IF-8 Exciter requirements
type: Interface
priority: should
verification: Test
component: Exciter
refines: SN-7
issues: STA-440, STA-462
guide: exciter

Stator shall read requirements from the REST API of a connected Exciter as the reading person, and refresh what it keeps of them when Exciter sends a signed delivery.

Rationale: A requirement block shows the wording that is current.

### IF-9 Single sign-on through Keycloak
type: Interface
priority: must
verification: Test
component: Identity
refines: SN-10
issues: STA-5, STA-411, STA-124
guide: working-in-the-suite

Stator shall sign people in with OpenID Connect through the realm dynamo of the suite's Keycloak and map the groups of the identity provider to the roles of the organization.

Rationale: One sign-in serves all four tools and a person is the same person in each.
