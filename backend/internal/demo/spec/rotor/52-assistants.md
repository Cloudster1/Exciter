# Functional requirements / Assistants

How an assistant works with Rotor as a person.

### FR-AST-1 Let an assistant connect with a person's consent
type: Functional
priority: should
verification: Test
component: Assistants
refines: SN-6
issues: ROT-47, ROT-114
guide: assistants

When an assistant registers or names its client ID metadata document and its person allows it on Rotor's consent page,
Rotor shall let it list, read, check, start and cancel workflows and runs as that person, or only read where the person
ticked Only read.

### FR-AST-2 Never let an assistant decide an approval
type: Functional
priority: must
verification: Test
component: Assistants
refines: SN-6, SN-3
issues: ROT-47
guide: assistants

Rotor shall let an assistant read approvals and shall refuse it every way to decide one.

Rationale: An approval an assistant can grant is no stop for a person at all.

### FR-AST-3 Show and end an assistant's access
type: Functional
priority: should
verification: Demonstration
component: Assistants
refines: SN-6
issues: ROT-47
guide: assistants

While a person has connected assistants, Rotor shall list them in Settings with what each may do and let the person
disconnect any at once; administrators shall see and disconnect everybody's and shall switch assistants off for the
installation.
