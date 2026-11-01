# Test specification / Triggers

### TC-TRG-1 A schedule fires at its time
type: Test case
verifies: FR-TRG-1
automated: yes
result: passed

Preconditions: A workflow the tester may run and a schedule `*/15 9-17 * * 1-5` in Europe/Berlin.

| Action | Expected |
|---|---|
| Choose Show the next runs. | Five times in working hours of Berlin show. |
| Set the clock past a time. | One run starts as the tester. |
| Stop Rotor over two times and start it. | One run starts for the missed time and none for the earlier one. |

### TC-TRG-2 A signed event starts a run once
type: Test case
verifies: FR-TRG-2
automated: yes
result: passed

Preconditions: A trigger on `issue.created` of Armature and the tool's secret.

| Action | Expected |
|---|---|
| Deliver a signed event. | The answer is 202 and one run starts. |
| Deliver the same event again. | The answer is 200 and no second run starts. |
| Deliver it with a timestamp `age` old. | The answer is 401 and nothing starts. |

Parameters: age = 6 minutes, 1 hour

### TC-TRG-3 A condition leaves an event out
type: Test case
verifies: FR-TRG-3
automated: yes
result: passed

Preconditions: A trigger for the project `CP` with the condition `payload.internal` does not equal `true`.

| Action | Expected |
|---|---|
| Deliver an event of project `CP` with `internal` true. | No run starts. |
| Deliver one of `CP` without `internal`. | A run starts. |
| Deliver one of another project. | No run starts. |

### TC-TRG-4 A workflow does not start on its own write
type: Test case
verifies: FR-TRG-4
automated: yes
result: passed

Preconditions: A trigger on `comment.added` whose workflow adds a comment.

| Action | Expected |
|---|---|
| Let the run add its comment and deliver the event about it. | No run starts. |
| Read the trigger's last firing. | It says the event was skipped as the workflow's own write and names the run. |

### TC-TRG-5 Unattended writes need the owner's agreement
type: Test case
verifies: FR-TRG-5
automated: yes
result: passed

Preconditions: A workflow with a write after no approval, and a version that names a call as `unattended`.

| Action | Expected |
|---|---|
| Add a trigger to the first workflow. | The form refuses it. |
| Add one to the second and agree to the listed writes. | The trigger is accepted. |
| Save a version that names more unattended calls. | At the next firing the trigger is off and says why. |

### TC-TRG-6 Rotor's webhook registers itself in Armature
type: Test case
verifies: FR-TRG-6
automated: no
result: blocked
note: Not built yet; the webhook still has to be registered by hand at Armature.

Preconditions: An administrator who connects Armature.

| Action | Expected |
|---|---|
| Connect Armature. | A webhook with Rotor's address is registered there. |
