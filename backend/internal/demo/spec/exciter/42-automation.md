# Functional requirements / Automation

How other systems and assistants work with Exciter.

### FR-AUT-1 Offer a REST API and personal access tokens
type: Functional
priority: must
verification: Test
component: Automation
refines: SN-14
issues: EXC-22
guide: automation

When a person makes a personal access token, Exciter shall accept it as a bearer for that person's rights, limited to reading or to named projects if the person asked, and shall refuse every write of a read-only token before any handler.

Rationale: Scripts and pipelines need an interface that does no more than their maker may.

### FR-AUT-2 Let an assistant work over MCP
type: Functional
priority: should
verification: Test
component: Automation
refines: SN-14
relates: FR-AUT-1
issues: EXC-181, EXC-188, EXC-190
guide: automation

When an assistant is connected by a token or by OAuth consent, Exciter shall let it use the tools, resources and prompts it lists as the person it works for, and shall mark what it wrote with the token or app that wrote it.

Rationale: People are accountable for what an assistant does in their name, and a reader must see what it wrote.

### FR-AUT-3 Send events to webhooks
type: Functional
priority: should
verification: Test
component: Automation
refines: SN-14
relates: FR-AUT-1
issues: EXC-48, EXC-133
guide: automation

When something happens that an endpoint subscribed to, Exciter shall post a signed event to it as the endpoint's owner may read it, retry with backoff, and turn the endpoint off after a day of failing.

Rationale: Other systems follow changes without asking again and again.
