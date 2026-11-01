# Functional requirements / Calendars and capacity

How the days people can work are known.

### FR-CAP-1 Import a holiday calendar from an .ics file
type: Functional
priority: should
verification: Test
component: Capacity
refines: SN-5
issues: ARM-28, ARM-44, ARM-45
guide: capacity

When an administrator imports an .ics file into a holiday calendar, Armature shall make one holiday of each event, of the length the event gives, and shall import again without turning half days into whole days.

Rationale: Public holidays are published as .ics files; typing them again is slow and wrong half of the time.

### FR-CAP-2 Record absences without a reason
type: Functional
priority: must
verification: Test
component: Capacity
refines: SN-5
issues: ARM-29, ARM-39
guide: capacity

When a person records days away, Armature shall store the days and no reason, shall let a team manager change the absences of a team member, and shall show colleagues the days only.

Rationale: Capacity needs the days; the reason is the person's own.

### FR-CAP-3 Set scheduled hours against hours left per week
type: Functional
priority: should
verification: Test
component: Capacity
refines: SN-5
issues: ARM-31, ARM-41, ARM-43, ARM-51
guide: capacity

When a person opens the resources page of a project, Armature shall show for each week the scheduled hours against the hours left, per team in a scrum project and per team or person in a kanban project, and shall list the work it could not place.

Rationale: A team overbooked in week 14 should see it before week 14.
