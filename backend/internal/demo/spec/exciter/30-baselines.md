# Functional requirements / Baselines

How an agreed specification is frozen and compared.

### FR-BSL-1 Take a baseline
type: Functional
priority: must
verification: Test
component: Baselines
refines: SN-6
issues: EXC-37
guide: baselines

When a project administrator takes a baseline of a project or of a section, Exciter shall record for each live requirement its revision, place and status, and the trace links among them, in one consistent step.

Rationale: What was agreed must be shown again exactly, whatever has changed since.

### FR-BSL-2 Compare a baseline with the present or another baseline
type: Functional
priority: should
verification: Test
component: Baselines
refines: SN-6
relates: FR-BSL-1
issues: EXC-37, EXC-153
guide: baselines

When a person compares a baseline with the present specification or with another baseline, Exciter shall list the requirements added, removed and changed, the links that changed, and the matrix's changes.

Rationale: A release review asks what changed since the last one.

### FR-BSL-3 Sign a baseline
type: Functional
priority: should
verification: Test
component: Baselines
refines: SN-6
relates: FR-BSL-1
issues: EXC-205
guide: baselines

When the people a project names have signed a baseline, Exciter shall refuse every change to it and its deletion.

Rationale: A signed release is evidence.
