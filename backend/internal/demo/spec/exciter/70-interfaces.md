# Interfaces

What Exciter offers the other tools of the suite, and what it uses of them.

### IF-1 REST API with OpenAPI
type: Interface
priority: must
verification: Test
component: API
refines: SN-14
issues: EXC-22
guide: automation

Exciter shall serve its operations under `/api/v1` with a signed-in session or a bearer token, describe them in an OpenAPI document, and report its build, its version and the integration capabilities it lists at `GET /api/v1/meta`.

Rationale: The other tools and scripts find out what an Exciter offers by asking it.

### IF-2 Requirements for pages of Stator
type: Interface
priority: must
verification: Test
component: API
refines: SN-9
issues: EXC-140
guide: links-to-stator

Exciter shall list `requirement-embed@1` and `requirement-list@1`, so that a partner reads a requirement by its key, searches requirements, and lists a project's requirements narrowed by a query a window at a time as the viewing person.

Rationale: Stator draws a block of requirements on a page by asking Exciter for them.

### IF-3 Trace links and specifications
type: Interface
priority: should
verification: Test
component: API
refines: SN-9
issues: EXC-18, EXC-22
guide: traceability

Exciter shall list `trace-links@1` and `specifications@1`, so that a partner lists, makes and takes off trace links, lists the projects a caller may read, and reads each project's outline.

Rationale: Other tools show and edit traces without a second copy of them.

### IF-4 Token exchange for partner apps and narrower tokens
type: Interface
priority: must
verification: Test
component: API
refines: SN-9
issues: EXC-273, EXC-188
guide: automation

Exciter shall list `oauth.token-exchange@1`, so that an app of the suite trades a person's access token of the shared identity provider for a token of Exciter within that person's rights without a consent page, and so that a client trades a token of its own grant for one that does less, as Rotor does for a run.

Rationale: People should not paste tokens between tools, and a run should hold less access than its person.

### IF-5 Webhooks
type: Interface
priority: should
verification: Test
component: API
refines: SN-14
issues: EXC-48, EXC-133
guide: automation

Exciter shall list `webhooks@1` and `webhooks@2`, so that a partner manages endpoints, reads deliveries, sends one again and tests one, and an endpoint on envelope version 2 receives a CloudEvent in structured mode.

Rationale: The operations are those of Stator's webhooks, so one client serves the suite.

### IF-6 Model Context Protocol endpoint
type: Interface
priority: should
verification: Test
component: API
refines: SN-14
issues: EXC-181, EXC-183
guide: automation

Exciter shall serve `POST /mcp` in the protocol revisions 2025-11-25 and 2025-06-18, with the tools, resources and prompts its lists name.

Rationale: An assistant connects with an address alone.

### IF-7 ReqIF files
type: Interface
priority: should
verification: Demonstration
component: API
refines: SN-8
issues: EXC-44
guide: import-export

Exciter shall read and write ReqIF files with types, attributes, requirements, relations and the files their rich text shows.

Rationale: ReqIF is the file other requirements tools exchange.

### IF-8 Evidence from pipelines
type: Interface
priority: must
verification: Test
component: API
refines: SN-10
issues: EXC-200, EXC-201
guide: tests

Exciter shall accept JUnit results and requirement tags found in source from a pipeline with a token of the evidence scope, and answer the trace check with a verdict that `exciter-ci` turns into an exit code.

Rationale: Any pipeline, among them those of Rotor, can report its tests.

### IF-9 Identity from the suite's realm
type: Interface
priority: must
verification: Demonstration
component: API
refines: SN-15
issues: EXC-7, EXC-102
guide: projects

Exciter shall use the OIDC realm `dynamo` of the suite's Keycloak for sign-in, group sync and the audience of partner tokens, and shall list the person's groups as role grants.

Rationale: One sign-in serves the suite.

### IF-10 Remote links and issues of Armature
type: Interface
priority: must
verification: Test
component: API
refines: SN-9
issues: EXC-26, EXC-27, EXC-100
guide: links-to-armature

Exciter shall use `issue-embed@1`, `remote-links@1`, `webhooks@1` and `theme-export@1` of Armature, each only when Armature lists it, and shall take Armature's token exchange to connect a person at sign-in.

Rationale: Each feature degrades to a sentence when Armature is older.

### IF-11 Remote links on pages of Stator
type: Interface
priority: must
verification: Test
component: API
refines: SN-9
issues: EXC-40, EXC-42
guide: links-to-stator

Exciter shall use the pages, quick search and webhooks of Stator, and `page-remote-links@1` to put a link on a page, and shall label the page a requirement is described by so that Stator finds it.

Rationale: A page and a requirement find each other from either side.

### IF-12 Theme file
type: Interface
priority: could
verification: Demonstration
component: API
refines: SN-15
issues: EXC-59
guide: projects

Exciter shall list `theme-export@1`, so that a theme exported as one `armature-theme/1` file is imported by any of the three tools.

Rationale: A team keeps one look across the suite.
