# Test specification / Projects and workflows

### TC-PRJ-1 A new project has what its template names
type: Test case
verifies: FR-PRJ-1
automated: yes
result: passed

Preconditions: A global administrator and a scrum template.

| Action | Expected |
|---|---|
| Create a project from the scrum template with the key `key`. | The project exists with the key. |
| Open its board and its workflow settings. | The board and the template's workflows are there. |

Parameters: Parameters: key = TST, DEMO

### TC-PRJ-2 Pickers offer only the allowed types
type: Test case
verifies: FR-PRJ-2
automated: yes
result: passed

Preconditions: A project administrator and a library of five issue types.

| Action | Expected |
|---|---|
| Allow Bug and Task in the project's Issue types. | The tab lists the two types as allowed. |
| Open Create issue. | The type picker offers Bug and Task only. |

### TC-PRJ-3 A member reads the settings and changes nothing
type: Test case
verifies: FR-PRJ-3
automated: no
result: passed

Preconditions: A member with the user role.

| Action | Expected |
|---|---|
| Open the project's settings. | The tabs show, read only, with a line that names whom to ask. |
| Look for the Automation and Import tabs. | They are not shown. |

### TC-PRJ-4 An archived project comes back whole
type: Test case
verifies: FR-PRJ-4
automated: yes
result: passed

Preconditions: A project with issues, archived by its administrator.

| Action | Expected |
|---|---|
| Restore the project from the projects list. | The project is listed again with its issues and boards. |
| Restore the same project again. | Armature refuses and says that it is not archived. |
