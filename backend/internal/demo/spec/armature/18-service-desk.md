# Functional requirements / Service desk

How customers raise requests and are answered in time.

### FR-DSK-1 Let customers raise requests by portal and by mail
type: Functional
priority: must
verification: Test
component: Service desk
refines: SN-9
issues: ARM-72, ARM-74
guide: service-desk

When a customer submits a request in the portal or mails the desk, Armature shall create an issue of the chosen request type, in the desk's project, and shall thread later mail with the customer's earlier mail about it.

Rationale: A customer who has to pick a channel will pick the other one; threaded mail keeps one conversation in one place.

### FR-DSK-2 Keep internal notes from the customer
type: Functional
priority: must
verification: Test
component: Service desk
refines: SN-9
issues: ARM-287
guide: service-desk

When an agent adds an internal note to a request, Armature shall show the note to agents only, in the portal, in mail and in the answers meant for customers.

Rationale: Agents say things to each other that a customer should never read.

### FR-DSK-3 Move the response and resolution clocks with the request
type: Functional
priority: must
verification: Test
component: Service desk
refines: SN-9
issues: ARM-66, ARM-75
guide: service-desk

When a request's priority changes, the request moves to another project or an agent resolves it, Armature shall recalculate the response and resolution clocks in the same transaction, and shall stop the first-response clock when the agent replies or resolves.

Rationale: A clock that ignores a change reports a breach that did not happen or hides one that did.

### FR-DSK-4 Reopen a resolved request on the customer's reply
type: Functional
priority: should
verification: Test
component: Service desk
refines: SN-9
issues: ARM-64, ARM-65
guide: service-desk

When a customer replies to a resolved request, Armature shall move the request back by the workflow's own transition, but shall not treat an out-of-office reply as an answer.

Rationale: A reply to a resolved request means it was not resolved; an automatic reply means nothing.
