# Functional requirements / Git repositories

How code reaches the issue it is for.

### FR-GIT-1 Show commits, branches, pull requests and CI runs on the issue
type: Functional
priority: must
verification: Test
component: Git
refines: SN-8
issues: ARM-70, ARM-71
guide: git

When a commit, branch or pull request names an issue, Armature shall list it in the issue's development panel with the state of every CI check on it, and shall keep a merged branch listed after the host deleted it.

Rationale: A developer should not have to ask the host what became of an issue's change.

### FR-GIT-2 Carry out a smart command once
type: Functional
priority: must
verification: Test
component: Git
refines: SN-8
issues: ARM-67, ARM-68
guide: git

When a commit message holds a command that moves an issue, Armature shall carry it out on the commit's first arrival and shall not carry it out again for a squash or merge commit that repeats the message or for a later delivery about a merged pull request.

Rationale: Merging a branch must not close an issue a second time or reopen what somebody moved on.

### FR-GIT-3 Disconnect a repository only on purpose
type: Functional
priority: should
verification: Test
component: Git
refines: SN-8
issues: ARM-419, ARM-420
guide: git

When a person who may not administer a project looks at its repositories, Armature shall not show Connect, Rotate secret or Disconnect, and when an administrator disconnects, Armature shall ask first, naming the data it removes.

Rationale: Disconnecting removes commits, branches and pull requests at one click; it must not be there to be clicked by accident.
