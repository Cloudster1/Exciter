# Functional requirements / Tests

How test cases are written, planned, run and reported.

### FR-TST-1 Write a test case with steps and parameters
type: Functional
priority: must
verification: Test
component: Tests
refines: SN-10
issues: EXC-250
guide: tests

When a person writes a requirement of a type marked as test cases, Exciter shall keep its preconditions, steps of an action and an expected result, and parameters, with the revision, and shall refuse more than 100 steps.

Rationale: A test case is a requirement that is traced and reviewed like the others.

### FR-TST-2 Plan and start a test execution
type: Functional
priority: must
verification: Test
component: Tests
refines: SN-10
relates: FR-TST-1
issues: EXC-251
guide: tests

When a person who may edit starts an execution of a test plan against a baseline, a version and a configuration, Exciter shall pin each case at the revision the baseline holds, list the cases left out with the reason, and keep the execution unchanged when the plan changes later.

Rationale: A result means something only against the exact test that gave it.

### FR-TST-3 Record results
type: Functional
priority: must
verification: Test
component: Tests
refines: SN-10
relates: FR-TST-2
issues: EXC-251
guide: tests

While an execution is open, when its manager or a tester who may comment records a step, Exciter shall keep its outcome, the actual result and its files, shall work out the case's outcome from its steps, and shall refuse everyone else and every change once the execution is closed.

Rationale: Testers record under their own names, and a closed round is a record.

### FR-TST-4 Raise an issue from a failure
type: Functional
priority: should
verification: Test
component: Tests
refines: SN-10
relates: FR-TST-3, FR-ARM-3
issues: EXC-251, EXC-345
guide: tests

When a tester files a failed case as an issue, Exciter shall create it in Armature as the tester with the step, the expected and the actual result, and link it with the relation relates to the case and the requirements it verifies.

Rationale: A failure becomes work for developers without being typed again.

### FR-TST-5 Take test results from CI as verification
type: Functional
priority: must
verification: Test
component: Tests
refines: SN-2
relates: FR-MAT-1
issues: EXC-200, EXC-223, EXC-252
guide: tests

When a pipeline uploads JUnit results with a token of the evidence scope, Exciter shall land each case on the requirements it names at the revision current at upload, mark it outdated after a newer revision, and answer the trace check with pass or fail.

Rationale: Automated tests are most of the evidence; they must count without being re-entered.
