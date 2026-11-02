# Stakeholder needs

What the people who work with Rotor need from it.

### SN-1 Run a workflow on demand
type: Stakeholder need
priority: must

A developer needs to start a workflow by hand, follow its run while it happens and stop it when it goes wrong.

### SN-2 Write, keep and share workflows
type: Stakeholder need
priority: must

A workflow author needs to write a workflow, learn what is wrong with it before it runs, keep every version and share
it with a team without sharing their own access.

### SN-3 Keep a person in control of what a run writes
type: Stakeholder need
priority: must

A quality manager needs a run to stop and ask a named person before it changes anything in the tools, and to see what
it is about to write.

### SN-4 Act only as the person, never more
type: Stakeholder need
priority: must

A security officer needs every run to hold only the rights of the person who started it, for as long as the run lasts,
and no run to reach what that person may not.

### SN-5 Start work without pressing Run
type: Stakeholder need
priority: should

A team lead needs a workflow to start on a schedule or when an issue, requirement or page changes, without a loop in
which a workflow starts itself.

### SN-6 Use Rotor from an assistant
type: Stakeholder need
priority: should

A developer needs to list, start and follow workflows from an assistant such as Claude Code, without the assistant
being able to approve anything.

### SN-7 Bound cost and keep a record
type: Stakeholder need
priority: must

An administrator needs to choose the language models, bound what a run and a person may spend, and read a record of
who did what that nobody can change.

### SN-8 Keep planted text from steering a run
type: Stakeholder need
priority: must

A security officer needs text an attacker wrote in an issue or a page to be read by a run as data, never as an
instruction.
