# Functional requirements / Administration

How administrators set up, bound and audit the installation.

### FR-ADM-1 Choose the language model providers
type: Functional
priority: must
verification: Test
component: Administration
refines: SN-7
issues: ROT-20
guide: administration

When an administrator adds a provider (Anthropic, OpenAI, Azure OpenAI or an OpenAI-compatible server), Rotor shall list
its models, keep the key in OpenBao and nowhere else, take the price of each model and let the administrator test the
connection and pick a default model.

### FR-ADM-2 Bound what a person may use in a day
type: Functional
priority: should
verification: Test
component: Administration
refines: SN-7
issues: ROT-38
guide: administration

Where an administrator sets a daily limit of tokens, cost or concurrent runs for a person, Rotor shall stop a run that
would pass it and say why, and shall show each person what they used.

### FR-ADM-3 Keep an audit log nobody can change
type: Functional
priority: must
verification: Test
component: Administration
refines: SN-7
issues: ROT-36
guide: administration

Rotor shall record who did what and what it did on its own, shall let an administrator filter the log and export it as
CSV or JSON, shall record the export itself, and shall let nobody change or empty the log.

Rationale: A record an administrator could edit proves nothing to an auditor.

### FR-ADM-4 Wire the suite with a service account
type: Functional
priority: could
verification: Test
component: Administration
refines: SN-7
issues: ROT-168, ROT-181
guide: administration

When a service account of the realm holding the administrator group presents its token, Rotor shall let it add tools and
set their webhook secrets and shall refuse it every other route, naming what is missing.
