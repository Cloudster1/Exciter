# Interfaces

What Armature offers the other tools of the suite.

### IF-1 Remote links on issues
type: Interface
priority: must
verification: Test
component: API
refines: SN-2
issues: ARM-14
guide: ../exciter/links-to-armature

Armature shall keep the remote links another tool puts on an issue through its REST API, and shall list them on the
issue with their relation and the title the tool gave them.

Rationale: Exciter traces requirements to issues; a link only Exciter knows would not show where developers look.
