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

When a person who may administer a project renames one of its boards, Armature shall show the board under its new
name everywhere it lists the project's boards.

Rationale: A project with several boards is read by their names; a board that keeps a wrong name is opened by
mistake.

## Swimlanes

Swimlanes split a board's columns by a field of the issues.

### FR-BRD-2 Configure the swimlanes of the board being viewed
type: Functional
priority: should
verification: Test
component: Boards
refines: SN-1
relates: FR-BRD-1
issues: ARM-52
guide: boards

When a person configures swimlanes from a board, Armature shall change the swimlanes of that board and of no other
board of the project.
