# Test specification / Runs

### TC-RUN-1 Start a run and follow it to its end
type: Test case
verifies: FR-RUN-1
automated: yes
result: passed

Preconditions: A workflow the tester may run.

| Action | Expected |
|---|---|
| Start the workflow. | A run is queued. |
| Wait for the run. | The run ends as succeeded. |
