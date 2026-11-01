# Functional requirements / Comments and watching

How people talk about a page and hear of changes.

### FR-COM-1 Comment on a page or a passage
type: Functional
priority: must
verification: Test
component: Comments
refines: SN-4
issues: STA-22, STA-23
guide: comments-and-reactions

When a person who may comment writes a comment on a page, or on a passage of it they selected, Stator shall show it with its author to the readers of the page, let others reply, and let a person resolve and reopen a passage's thread.

Rationale: Questions about a sentence belong at the sentence.

### FR-COM-2 Delete a comment only as its author or a space administrator
type: Functional
priority: should
verification: Test
component: Comments
refines: SN-4
issues: STA-180
guide: comments-and-reactions

Stator shall let only the author of a comment and the administrators of its space delete it.

Rationale: A comment is a person's words; others may not take them away, except to keep order.

### FR-COM-3 Mention a person
type: Functional
priority: should
verification: Test
component: Comments
refines: SN-4
issues: STA-24
guide: comments-and-reactions

When a person mentions another in a page or a comment, Stator shall tell the mentioned person, provided they may view the page.

Rationale: A question to a named person should reach them.

### FR-COM-5 Watch a page or a space
type: Functional
priority: should
verification: Test
component: Notifications
refines: SN-4
issues: STA-25
guide: watching-and-notifications

When a person watches a page or a space, Stator shall tell them in the application and by email when a page they watch is published, moved, deleted or commented on.

Rationale: People should not have to open a page again and again to see whether it changed.

### FR-COM-6 Tell only those who may read
type: Functional
priority: must
verification: Test
component: Notifications
refines: SN-4
issues: STA-26
guide: watching-and-notifications

Stator shall never send a notification, in the application or by email, that names a page its recipient may not view.

Rationale: A notification is a way to learn what a page says.
