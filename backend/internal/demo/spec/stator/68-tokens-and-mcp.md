# Functional requirements / Tokens and assistants

How scripts and assistants act as a person.

### FR-API-1 Make a personal access token
type: Functional
priority: must
verification: Test
component: Tokens
refines: SN-8
issues: STA-21, STA-111
guide: tokens-and-mcp

When a person makes a token, Stator shall show its secret once, let the person limit it to reading or to chosen spaces and give it a lifetime, let the person revoke it, and let an administrator revoke any token.

Rationale: A script should hold no more power than it needs and be stoppable.

### FR-API-2 A token never does more than its owner
type: Functional
priority: must
verification: Test
component: Tokens
refines: SN-8
issues: STA-21, STA-499
guide: tokens-and-mcp

Stator shall answer a request with a token as it answers its owner, refuse every write of a read-only token, and give a connected app a narrower token for one run that reaches no more than the app's grant.

Rationale: A token that outgrows its owner is a way around the permissions.

### FR-API-3 Offer the wiki to an assistant over MCP
type: Functional
priority: should
verification: Test
component: MCP
refines: SN-8
issues: STA-106, STA-449, STA-461
guide: tokens-and-mcp

Stator shall offer its reading operations, and for a token that may write its writing ones, as tools of an MCP endpoint, let an app connect by OAuth, and refuse a tool call exactly where the same HTTP call is refused.

Rationale: Assistants read and write the wiki; they must not get around its rules.

### FR-API-4 Tell other systems by webhook
type: Functional
priority: should
verification: Test
component: Webhooks
refines: SN-8
issues: STA-110, STA-412, STA-531
guide: webhooks

When a page is published, moved or deleted, a comment is created or a remote link is taken off, Stator shall send a signed, numbered delivery to each endpoint subscribed to that topic, try a failed delivery again, and show every attempt in a log.

Rationale: A tool that must react to the wiki should not have to ask it again and again.
