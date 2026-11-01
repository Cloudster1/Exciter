# Functional requirements / Armature links

How requirements are traced to the issues of Armature.

### FR-ARM-1 Connect Armature
type: Functional
priority: must
verification: Test
component: Armature
refines: SN-9
issues: EXC-26, EXC-100
guide: links-to-armature

When an administrator names the address of an Armature, Exciter shall ask what it offers, keep the answer with the connection, check the capability a feature needs before it calls, and say in a sentence which feature is off and what to update.

Rationale: A tool of another release must not break the one beside it.

### FR-ARM-2 Trace a requirement to issues
type: Functional
priority: must
verification: Test
component: Armature
refines: SN-9
relates: FR-ARM-1
issues: EXC-27, EXC-297
guide: links-to-armature

When a person links a requirement to an issue as implements, verifies or relates, Exciter shall show the issue with its status as the viewer may read it, and shall put a remote link with that relation on the issue in Armature.

Rationale: Developers look at issues, not at requirements; the link must be where they look.

### FR-ARM-3 Create an issue from a requirement
type: Functional
priority: should
verification: Test
component: Armature
refines: SN-9
relates: FR-ARM-2
issues: EXC-28
guide: links-to-armature

When a person who has connected Armature creates an issue from a requirement, Exciter shall create it in the project they pick, in their name, and link it to the requirement.

Rationale: The first issue of a requirement should not be typed again.

### FR-ARM-4 Keep linked issues' statuses current
type: Functional
priority: should
verification: Test
component: Armature
refines: SN-2
relates: FR-ARM-2
issues: EXC-29
guide: links-to-armature

When Armature posts a signed webhook for a linked issue, Exciter shall store the issue's new status category for the links that name it, and shall refuse a post whose signature does not match.

Rationale: Coverage counts an issue as done only when Armature says so.
