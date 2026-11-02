# Functional requirements / Boards

How work moves across a board.

### FR-BRD-1 Rename a board
type: Functional
priority: should
verification: Test
component: Boards
refines: SN-1
issues: ARM-52
guide: boards

When a person who may administer a project renames one of its boards, Armature shall show the board under its new name everywhere it lists the project's boards.

Rationale: A project with several boards is read by their names; a board that keeps a wrong name is opened by mistake.

### FR-BRD-2 Configure the swimlanes of the board being viewed
type: Functional
priority: should
verification: Test
component: Boards
refines: SN-1
relates: FR-BRD-1
issues: ARM-52, ARM-119
guide: boards

When a person configures swimlanes from a board, Armature shall change the swimlanes of that board and of no other board of the project.

Rationale: A team with a board per stream sets each stream's lanes on their own.

### FR-BRD-3 Let finished work leave a kanban board
type: Functional
priority: should
verification: Test
component: Boards
refines: SN-1
issues: ARM-54
guide: boards

When finished work is older than the number of days a kanban board sets, Armature shall leave it off the board and shall keep counting only what stays.

Rationale: A done lane that only grows hides the work in progress and makes the work-in-progress count meaningless.

### FR-BRD-4 Narrow a board with quick filters
type: Functional
priority: could
verification: Test
component: Boards
refines: SN-1
issues: ARM-63
guide: boards

When a person chooses a quick filter on a board (mine, an assignee, a label, a type, an epic or a text), Armature shall show only the matching cards and shall leave the issues themselves unchanged.

Rationale: A board of sixty cards is read by looking for one's own.
