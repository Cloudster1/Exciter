# Functional requirements / Notifications and automation

How people and other systems are told, and what happens by itself.

### FR-NTF-1 Switch notifications off per kind and channel
type: Functional
priority: should
verification: Test
component: Notifications
refines: SN-7
issues: ARM-410, ARM-411
guide: notifications

When a person turns off the inbox for a kind of notification, Armature shall stop sending that kind to the inbox and shall not stop its mail, and shall offer switches only for kinds it sends.

Rationale: Switching off a channel should not silence the other; a switch that does nothing teaches people to ignore settings.

### FR-NTF-2 Deliver webhooks, retry them and keep one slow endpoint from holding up the rest
type: Functional
priority: must
verification: Test
component: Notifications
refines: SN-7
issues: ARM-99, ARM-20
guide: notifications

When an event reaches a topic an endpoint takes, Armature shall post it signed, shall try a failed delivery again after 1 minute, 5 minutes, 30 minutes, 2 hours and 12 hours, and shall let a slow endpoint delay only its own deliveries.

Rationale: A receiver that is down for an hour should miss nothing, and one slow receiver should not stop the others.

### FR-NTF-3 Run an automation rule by hand under its conditions
type: Functional
priority: should
verification: Test
component: Notifications
refines: SN-7
issues: ARM-402, ARM-401
guide: notifications

When a project administrator runs a rule by hand, Armature shall check the rule's conditions and the rights of its account before it acts.

Rationale: A test run that skips the conditions proves nothing about the rule.
