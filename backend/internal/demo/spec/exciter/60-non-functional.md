# Non-functional requirements

### NFR-1 Coverage answers quickly
type: Non-functional
priority: should
verification: Test
component: Coverage
refines: SN-2
guide: matrix-and-coverage

Exciter shall answer the coverage of a project of 5,000 requirements within 2 seconds at the 95th percentile, with the stack running on the development compose file.

| Requirements | Answer within |
|---|---|
| 5,000 | 2 s |

Rationale: A lead looks at coverage in a meeting; a slow answer is not looked at.

### NFR-2 The specification opens fast
type: Non-functional
priority: should
verification: Test
component: Specification
refines: SN-3
issues: EXC-477
guide: documents

Exciter shall answer the first window of 100 entries of a specification of 5,000 requirements within 1 second at the 95th percentile, and shall serve a larger specification by windows so that no single answer outlasts the request limit.

| Requirements | First window within |
|---|---|
| 1,000 | 0.5 s |
| 5,000 | 1 s |

Rationale: Writers open the specification many times a day; a large one must not stop them.

### NFR-3 What a person may not view does not show
type: Non-functional
priority: must
verification: Test
component: Access
refines: SN-4
issues: EXC-12, EXC-330
guide: projects

Exciter shall answer 404 and not 403 for a project, requirement, baseline or file the caller may not view, in 100 percent of the operations that read one by an identifier, and shall count in no figure what the reader may not see.

Rationale: A refusal that says it exists tells the reader what they may not know.

### NFR-4 Pages meet the accessibility baseline
type: Non-functional
priority: should
verification: Test
component: Web client
refines: SN-15
issues: EXC-9
guide: getting-started

Exciter shall show no serious or critical violation of the accessibility checker on any page of the browser suite's accessibility run, in the light and the dark theme.

Rationale: People who use a keyboard or a screen reader work with requirements too.

### NFR-5 German and English are complete
type: Non-functional
priority: should
verification: Test
component: Web client
refines: SN-15
issues: EXC-332
guide: getting-started

Exciter shall offer every string of its web client in English and in German, so that the German catalogue misses 0 of the English catalogue's strings, and shall write exports in the language of the reader.

Rationale: A team works in the language it thinks in.

### NFR-6 Uploads and links are bounded
type: Non-functional
priority: must
verification: Test
component: Limits
refines: SN-15
issues: EXC-21, EXC-250, EXC-251, EXC-18
guide: tests

Exciter shall refuse a file of an attachment or a test step over 50 MB, a test case of more than 100 steps, a test plan of more than 500 cases, and a requirement end of more than 500 trace links, each with a sentence that names the limit.

| What | Limit |
|---|---|
| File | 50 MB |
| Steps of a test case | 100 |
| Cases of a test plan | 500 |
| Links at one end | 500 |

Rationale: A limit that is written down is one a person can plan for and an attacker cannot exceed.

### NFR-7 Webhook delivery is retried and ends
type: Non-functional
priority: should
verification: Test
component: Webhooks
refines: SN-14
issues: EXC-48, EXC-460
guide: automation

Exciter shall retry a failed webhook delivery with growing waits, shall turn an endpoint off after 24 hours of failing, and shall not post a delivery twice when more than one sender runs.

Rationale: A receiver that is down for an hour should catch up, and one gone for good should stop costing work.

### NFR-8 Secrets are kept sealed
type: Non-functional
priority: must
verification: Test
component: Security
refines: SN-15
issues: EXC-22, EXC-26
guide: automation

Exciter shall store a personal access token only as its SHA-256 digest, seal a person's tokens for Armature and Stator with the installation's key, and refuse an outbound address inside the network that the administrator did not name.

Rationale: A leaked database must not hand out access to people's tools.
