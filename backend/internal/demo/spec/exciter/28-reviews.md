# Functional requirements / Reviews

How requirements are discussed, reviewed and approved.

### FR-REV-1 Comment, mention and watch
type: Functional
priority: should
verification: Test
component: Reviews
refines: SN-5
issues: EXC-33, EXC-34
guide: reviews

When a person comments on a requirement and mentions someone, or watches a requirement, Exciter shall tell the mentioned and the watchers in the app and by email according to each person's notification settings.

Rationale: Discussion about a requirement belongs next to it, and the people concerned must hear of it.

### FR-REV-2 Review and approve a revision
type: Functional
priority: must
verification: Test
component: Reviews
refines: SN-5
relates: FR-REV-1
issues: EXC-35
guide: reviews

When an editor asks for a review of a revision and the project has an approval rule for its type, Exciter shall pin that revision, take decisions only from the approvers the rule names, and move the requirement along only when the rule's count is met.

Rationale: An approval means one revision was approved by named people; a later edit must not inherit it.

### FR-REV-3 Sign an approval
type: Functional
priority: should
verification: Test
component: Reviews
refines: SN-5
relates: FR-REV-2
issues: EXC-46
guide: reviews

Where a project requires signed approvals, when a reviewer decides, Exciter shall ask them to confirm the meaning of the signature and to prove who they are again, and shall keep the meaning, the revision, its content hash and the proof.

Rationale: An audit asks who approved what and how they proved it.

### FR-REV-4 Review a section or a baseline as one session
type: Functional
priority: should
verification: Test
component: Reviews
refines: SN-5
relates: FR-REV-2
issues: EXC-204, EXC-215
guide: reviews

When a person opens a review session on a section, a baseline or a query, Exciter shall make a review of each requirement for the people it names, run it in rounds, and close it with a report.

Rationale: A release is reviewed as a whole by many people.
