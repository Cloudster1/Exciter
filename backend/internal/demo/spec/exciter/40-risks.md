# Functional requirements / Risks

How risks are rated and their mitigations followed.

### FR-RSK-1 Rate a risk before and after mitigation
type: Functional
priority: could
verification: Test
component: Risks
refines: SN-13
issues: EXC-259
guide: risks

When a person rates a requirement of a type marked as risks, Exciter shall work out the level before and after mitigation from the project's scales and matrix or priority number, and keep the rating in the revision.

Rationale: A risk is judged by what it is worth before and after what is done about it.

### FR-RSK-2 Show the FMEA table and the risk matrix
type: Functional
priority: could
verification: Test
component: Risks
refines: SN-13
relates: FR-RSK-1
issues: EXC-260, EXC-261
guide: risks

While a mitigation requirement is implemented and verified, Exciter shall count it as in effect, and shall show the FMEA table, the risk matrix and the risk report with only mitigations in effect lowering a risk.

Rationale: A planned mitigation is not a risk reduced.
