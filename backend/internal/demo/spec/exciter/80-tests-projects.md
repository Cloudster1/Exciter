# Test specification / Projects

### TC-PRJ-1 Make a project from the Software specification template
type: Test case
verifies: FR-PRJ-1
automated: yes
result: passed

Preconditions: Alice, an administrator, is signed in.

| Action | Expected |
|---|---|
| Make a project with the key `TPL` from the Software specification template. | The project exists and has the template's types and starter sections. |
| Add a requirement type to the template's copy in a second project made later, then open `TPL`. | `TPL` does not list the added type. |

### TC-PRJ-2 A role holds only what it grants
type: Test case
verifies: FR-PRJ-2, FR-PRJ-3
automated: yes
result: passed

Preconditions: A project with a requirement. Bob holds the role `role` through the group Everyone only.

Parameters: role = viewer, reviewer, editor; outcome = refused, refused, saved

| Action | Expected |
|---|---|
| Bob opens the requirement and tries to edit its statement. | The outcome is `outcome`. |
| An administrator archives the project and Bob tries to edit again. | The change is refused because the project is archived. |

### TC-PRJ-3 Sign in with single sign-on
type: Test case
verifies: FR-PRJ-4
automated: yes
result: passed

Preconditions: The development stack with its Keycloak realm.

| Action | Expected |
|---|---|
| Choose Continue with single sign-on and sign in as alice. | Alice reaches the home page. |
| Open the identity provider's link back in a second browser. | Nobody is signed in there and Exciter asks to start again. |
