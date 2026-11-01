# Functional requirements / Triggers

How a workflow starts without anyone pressing Run.

### FR-TRG-1 Start a workflow on a schedule
type: Functional
priority: should
verification: Test
component: Triggers
refines: SN-5
issues: ROT-34
guide: triggers

When a trigger's cron fields, read in its time zone, name a time, Rotor shall start a run as the trigger's owner, shall
skip a time that does not exist when summer time starts and fire a time that happens twice only once, and shall fire a
time missed while Rotor was down once.

### FR-TRG-2 Start a workflow from an event of a tool
type: Functional
priority: should
verification: Test
component: Triggers
refines: SN-5
issues: ROT-35
guide: triggers

When Armature, Exciter or Stator delivers an event signed with the tool's secret within five minutes, Rotor shall start
the matching triggers once, however often the tool delivers it, and shall refuse a delivery with a wrong or old signature.

Rationale: A delivery that could be recorded and played back would let anyone start a run.

### FR-TRG-3 Narrow a trigger by project and condition
type: Functional
priority: should
verification: Test
component: Triggers
refines: SN-5
issues: ROT-126
guide: triggers

Where an event trigger names projects or conditions on the event, Rotor shall start a run only for an event of those
projects whose values meet every condition.

### FR-TRG-4 Keep a workflow from starting itself
type: Functional
priority: must
verification: Test
component: Triggers
refines: SN-5
issues: ROT-127
guide: triggers

When an event reports a change the same workflow wrote within the last ten minutes, Rotor shall not start the workflow
again and shall say on the trigger that it skipped the event as the workflow's own write.

### FR-TRG-5 Allow unattended runs only for approved writes
type: Functional
priority: must
verification: Test
component: Triggers
refines: SN-3, SN-5
issues: ROT-128
guide: triggers

Where a workflow starts from a trigger, Rotor shall accept the trigger only if every node that may write comes after an
approval or writes a call the owner agreed to as unattended, and shall turn the trigger off, with the reason, when a
new version asks for more.

Rationale: Nobody watches a triggered run, so the agreement must be the owner's and must not grow unnoticed.

### FR-TRG-6 Register Rotor's webhook in a tool by itself
type: Functional
priority: could
verification: Demonstration
component: Triggers
refines: SN-5
issues: ROT-84
guide: triggers

When an administrator connects Armature, Rotor shall register its webhook there and keep the secret, so that no one
copies an address and a secret by hand.
