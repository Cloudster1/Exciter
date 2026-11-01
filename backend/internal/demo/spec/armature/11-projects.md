# Functional requirements / Projects and workflows

How a project is set up and what moves its issues.

### FR-PRJ-1 Start a project from a template
type: Functional
priority: must
verification: Test
component: Projects
refines: SN-4
issues: ARM-467, ARM-468
guide: projects

When a global administrator creates a project from a template, Armature shall give the project the template's kind, first board, workflows and pages.

Rationale: A team that starts from a template is working the same day; one that starts from nothing builds a board and a workflow first.

### FR-PRJ-2 Allow issue types per project from one library
type: Functional
priority: must
verification: Test
component: Projects
refines: SN-4
issues: ARM-464, ARM-465, ARM-466
guide: projects

When a project administrator allows a set of issue types from the installation's library, Armature shall offer only those types in every picker of that project.

Rationale: Offering every type of the installation in every project makes a short list long and invites wrong types.

### FR-PRJ-3 Show every member how the project is set up
type: Functional
priority: should
verification: Inspection
component: Projects
refines: SN-4
issues: ARM-471, ARM-463
guide: projects

When a member who may not administer a project opens its settings, Armature shall show the settings read only, with a line that says whom to ask, and shall not show the Automation and Import tabs.

Rationale: A member who sees why a board is as it is asks the right person; hidden settings make a project look arbitrary.

### FR-PRJ-4 Restore an archived project
type: Functional
priority: should
verification: Test
component: Projects
refines: SN-4
issues: ARM-112, ARM-423
guide: projects

When a project administrator restores an archived project, Armature shall bring back its issues, boards and settings as they were, and shall answer a restore of a project that is not archived with a refusal that says so.

Rationale: A project archived by mistake should not mean lost work.
