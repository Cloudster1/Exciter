# Interfaces

What Armature offers the other tools of the suite, and what it uses of them.

### IF-1 Remote links on issues
type: Interface
priority: must
verification: Test
component: API
refines: SN-2
issues: ARM-14, ARM-223
guide: integrations

Armature shall keep the remote links another tool puts on an issue through its REST API, with the kind of thing each is and its relation, and shall list them on the issue with the title the tool gave them.

Rationale: Exciter traces requirements to issues; a link only Exciter knows would not show where developers look.

### IF-2 Report what the build offers
type: Interface
priority: must
verification: Test
component: API
refines: SN-11
issues: ARM-238, ARM-239
guide: integrations

Armature shall answer GET /api/v1/meta without a sign-in with the build, the API version and the sorted list of its capabilities, each a name with a version.

Rationale: A partner decides from a capability name whether to rely on a feature, and asks before it has an account.

### IF-3 Signed webhooks in two envelope versions
type: Interface
priority: must
verification: Test
component: API
refines: SN-7
issues: ARM-242, ARM-286, ARM-224, ARM-18
guide: integrations

Armature shall post an event in the envelope version the endpoint was registered for, version 1 or version 2 as a CloudEvents 1.0 event, and shall name the status categories in the payload of a transition.

Rationale: Stator and Exciter read Armature's events, and a shared shape lets a broker read them without code for Armature.

### IF-4 Sign in through the suite's Keycloak realm
type: Interface
priority: must
verification: Test
component: Identity
refines: SN-10
issues: ARM-241, ARM-325, ARM-319
guide: access

Armature shall sign people in through OpenID Connect against the suite's one realm, with PKCE, and shall sign in silently a person who already has a session of that realm.

Rationale: One sign-in for Armature, Exciter, Stator and Rotor is what makes the suite feel like one product.

### IF-5 Connected apps and token exchange
type: Interface
priority: should
verification: Test
component: API
refines: SN-10
issues: ARM-280, ARM-298, ARM-302
guide: integrations

Armature shall give a connected app, once the person consents, a token narrower than the person's rights, and shall exchange a token of the suite's identity provider for one of its own.

Rationale: Stator and Exciter act in a person's name; they should never hold more than the person may do.

### IF-6 Work on issues over MCP and clone through the git proxy
type: Interface
priority: should
verification: Test
component: API
refines: SN-8
issues: ARM-305, ARM-332, ARM-333, ARM-352
guide: integrations

Armature shall offer the operations of its API marked for models at one MCP endpoint, as the person whose token it holds, and shall forward an issue branch's clone and push to the repository's host without giving out the host's token.

Rationale: Rotor writes code for an issue as the person who asked, and a host's token must never leave Armature.

### IF-7 Read issues and ask for access from another tool
type: Interface
priority: should
verification: Test
component: API
refines: SN-2
issues: ARM-279
guide: integrations

Armature shall answer a partner's read of an issue by key and its search by NQL as the person whose token it holds, and shall take a person's request for access to an issue through POST /api/v1/issues/{key}/access-requests.

Rationale: Stator embeds an issue in a page for everyone who reads it, including readers who may not see the issue.

### IF-8 Link a project to its documentation page
type: Interface
priority: could
verification: Inspection
component: Web
refines: SN-2
issues: ARM-15
guide: integrations

Armature shall show a project's documentation link in its sidebar, so that the Stator page of a project is one click away.

Rationale: The people in a project look for its guide where the project's other pages are.
