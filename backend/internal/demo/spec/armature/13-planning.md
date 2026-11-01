# Functional requirements / Planning

How the backlog becomes sprints and a timeline.

### FR-PLN-1 Complete a sprint and carry its unfinished work
type: Functional
priority: must
verification: Test
component: Planning
refines: SN-5
issues: ARM-400, ARM-62
guide: planning

When a scrum master completes a sprint, Armature shall move each unfinished issue to the place the scrum master chose, including the next sprint, and shall keep the report of the completed sprint as it was.

Rationale: A sprint that cannot be closed with its leftovers stays open for ever, and a report that changes afterwards is worth nothing.

### FR-PLN-2 Draw a burndown that matches the sprint's days
type: Functional
priority: should
verification: Test
component: Planning
refines: SN-5
issues: ARM-60, ARM-61
guide: planning

When a person opens the burndown of a sprint, Armature shall draw a point for each day of the sprint, including its first, and shall count an issue as remaining only from the day it was filed.

Rationale: A burndown that starts late or counts work before it existed misleads the team about its pace.

### FR-PLN-3 Refuse dependencies that go round in a circle
type: Functional
priority: must
verification: Test
component: Planning
refines: SN-5
issues: ARM-350
guide: planning

When a person adds a blocks link that would close a circle, Armature shall refuse it, and when an issue depends on a parent, Armature shall hold the parent's children back as well.

Rationale: A circle can never be finished, and a parent's date means little if its children start before it.

### FR-PLN-4 Propose, apply and undo an automatic plan
type: Functional
priority: should
verification: Test
component: Planning
refines: SN-5
issues: ARM-345, ARM-346, ARM-347, ARM-348, ARM-349
guide: planning

When a person chooses Auto plan on the timeline, Armature shall show a preview of every move that starts each blocked issue after its blockers, shall apply the moves in one transaction and refuse them if an issue moved since the preview, and shall undo them in one step.

Rationale: Dates moved one by one are slow and error-prone; a preview with one apply and one undo makes the proposal safe to try.
