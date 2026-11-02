# Test specification / Matrix and coverage

### TC-MAT-1 Coverage follows the rule and the links
type: Test case
verifies: FR-MAT-1
automated: yes
result: passed

Preconditions: A rule asking `verified` for the type Functional. Three requirements of that type with no link, one verifying test, and a verifying link that is suspect.

| Action | Expected |
|---|---|
| Open the project's coverage. | The requirement with no link is none, the one with a test is full, and the one with the suspect link is partial. |
| Clear the suspect mark. | That requirement becomes full. |

### TC-MAT-2 The matrix shows gaps
type: Test case
verifies: FR-MAT-2, FR-MAT-3
automated: yes
result: passed

Preconditions: Ten requirements and five tests, seven pairs linked.

| Action | Expected |
|---|---|
| Open the matrix with the tests as columns and show the gaps. | Rows without a test are marked. |
| Select an empty cell and make the link. | The link exists on the requirement and the gap goes. |

### TC-MAT-3 The chain report names broken chains
type: Test case
verifies: FR-MAT-4
automated: yes
result: passed

Preconditions: A trace model of Need, Functional, Test and a Need with no Functional below it.

| Action | Expected |
|---|---|
| Open the chain report. | The Need with nothing below it is reported as broken at its first step. |
