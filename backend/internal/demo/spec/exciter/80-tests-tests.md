# Test specification / Tests

### TC-TST-1 A test case holds steps and parameters
type: Test case
verifies: FR-TST-1
automated: yes
result: passed

Preconditions: A project with a test case type.

| Action | Expected |
|---|---|
| Write a test case with two steps and a parameter of three values. | The case keeps them in a revision. |
| Save a case with 101 steps. | It is refused with a sentence that gives the limit of 100. |

### TC-TST-2 An execution is pinned at its start
type: Test case
verifies: FR-TST-2
automated: yes
result: passed

Preconditions: A plan of two cases and a baseline holding one of them.

| Action | Expected |
|---|---|
| Start an execution against the baseline. | One case runs at the baseline's revision and the other is listed as left out with the reason. |
| Edit the plan. | The execution is unchanged. |

### TC-TST-3 Record and close an execution
type: Test case
verifies: FR-TST-3
automated: yes
result: passed

Preconditions: An open execution with Bob as tester.

| Action | Expected |
|---|---|
| Bob records one step as failed and the others as passed. | The case is failed. |
| Dave, who is no tester, records a result. | It is refused. |
| The manager closes the execution and Bob records again. | The record is refused as closed. |

### TC-TST-4 File a failed case as an issue
type: Test case
verifies: FR-TST-4
automated: yes
result: passed

Preconditions: A failed case and a connected Armature.

| Action | Expected |
|---|---|
| File the case as an issue. | The issue has the step, the expected and the actual result. |
| Open the case and a requirement it verifies. | Both show the issue linked as relates. |

### TC-TST-5 CI results count as verification
type: Test case
verifies: FR-TST-5
automated: yes
result: passed

Preconditions: A requirement `key` verified by no test, and a JUnit file that names it in a test.

| Action | Expected |
|---|---|
| Upload the file with an evidence token. | The requirement shows a passing test at its current revision. |
| Edit the requirement. | The result shows as outdated. |
| Ask the trace check. | It fails on the requirement whose result is outdated. |
