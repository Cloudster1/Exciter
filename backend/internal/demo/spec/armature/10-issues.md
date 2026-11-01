# Functional requirements / Issues

What an issue holds and how it changes.

### FR-ISS-1 Keep the hierarchy one level at a time
type: Functional
priority: must
verification: Test
component: Issues
refines: SN-3
issues: ARM-81
guide: issues

When a person sets the parent of an issue, Armature shall accept a parent exactly one level above the issue (initiative over epic over story over subtask) and shall refuse any other.

Rationale: A hierarchy that skips levels cannot be rolled up into progress, and the rule has to hold whoever writes the issue.

### FR-ISS-2 Refuse a save made from an outdated copy
type: Functional
priority: must
verification: Test
component: Issues
refines: SN-3
issues: ARM-89
guide: issues

When a person saves an issue that somebody else saved after the person opened it, Armature shall refuse the save, shall say so and shall keep the other person's change.

Rationale: Two people editing one description would otherwise lose the earlier work without a word.

### FR-ISS-3 Change the type of an issue from its page
type: Functional
priority: should
verification: Test
component: Issues
refines: SN-3
issues: ARM-114, ARM-85
guide: issues

When a person changes the type of an issue on its page to a type with another workflow, Armature shall keep the issue and shall record in its history the status it moves to.

Rationale: A wrongly filed bug should not have to be copied into a story; the history keeps the change honest.

### FR-ISS-4 Change many issues at once and report what happened
type: Functional
priority: should
verification: Test
component: Issues
refines: SN-3
issues: ARM-91, ARM-84
guide: issues

When a person applies a bulk edit to ticked issues, Armature shall change every issue it can and shall list, for each issue it could not change, the reason, leaving that issue as it was.

Rationale: A bulk edit that stops silently half way leaves a team unsure which issues changed.
