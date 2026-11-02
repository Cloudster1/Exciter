# Test specification / Stator links

### TC-STA-1 Saving the connection records the remote links capability
type: Test case
verifies: FR-STA-1
automated: yes
result: passed

Preconditions: A Stator that lists `page-remote-links@1`.

| Action | Expected |
|---|---|
| Save the connection as an administrator. | The connection records that Stator takes remote links. |

### TC-STA-2 A page published again makes the link suspect
type: Test case
verifies: FR-STA-2
automated: yes
result: passed

Preconditions: A requirement linked to a page.

| Action | Expected |
|---|---|
| Publish the page again in Stator and let its webhook arrive. | The link shows as suspect. |
| Move the page to another space. | The link shows the page in its new space. |

### TC-STA-3 The page shows the requirement and a late event changes nothing
type: Test case
verifies: FR-STA-2, FR-STA-3
automated: yes
result: failed
note: In one run of three a late event moved the link back to the old space. See the issue filed for the webhook order.

Preconditions: A requirement linked to a page.

| Action | Expected |
|---|---|
| Open the page in Stator. | It carries a link to the requirement. |
| Deliver an old `page.published` event after a move. | The link still names the page's new space. |
