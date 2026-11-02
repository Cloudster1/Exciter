# Test specification / Quality

### TC-QLT-1 A vague word is marked
type: Test case
verifies: FR-QLT-1
automated: yes
result: passed

Preconditions: A rule that marks the word `fast`.

| Action | Expected |
|---|---|
| Write a statement with the word `fast`. | The word is marked with how to fix it and the score falls. |

### TC-QLT-2 A changed definition makes requirements suspect
type: Test case
verifies: FR-QLT-2
automated: yes
result: passed

Preconditions: A term `Operator` used in a requirement.

| Action | Expected |
|---|---|
| Open the requirement. | The term is marked. |
| Change the term's definition. | The requirement is suspect. |

### TC-QLT-3 Likely duplicates are offered and merged
type: Test case
verifies: FR-QLT-3
automated: yes
result: passed

Preconditions: Two requirements with nearly the same statement.

| Action | Expected |
|---|---|
| Open the page of likely pairs. | The pair is listed. |
| Merge the pair. | One requirement remains with the links of both. |
