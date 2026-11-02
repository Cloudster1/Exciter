# Non-functional requirements

### NFR-1 Pages answer quickly
type: Non-functional
priority: should
verification: Test
component: Performance
refines: SN-11
guide: issues

Armature shall answer the operations below within the time given, at the 95th percentile, on the development compose file with 10,000 issues in the installation.

| Operation | Answer within |
|---|---|
| Open an issue | 1 s |
| Search by NQL, first page | 2 s |
| Open a board of 200 cards | 2 s |
| Open the plan of a project | 3 s |

Rationale: A tracker is opened many times a day; a slow page is paid for at every visit.

### NFR-2 Long lists do not load everything
type: Non-functional
priority: should
verification: Test
component: Performance
refines: SN-11
issues: ARM-261, ARM-273, ARM-274, ARM-276
guide: projects

Armature shall load a list of projects, members or labels a page or a match at a time, so that an installation of 5,000 projects opens the projects list in 2 seconds at the 95th percentile.

Rationale: Lists that load every row made the sidebar and the pickers slow when installations grew.

### NFR-3 The web app can be used by everyone
type: Non-functional
priority: should
verification: Analysis
component: Usability
refines: SN-11
issues: ARM-17, ARM-275, ARM-343
guide: getting-started

Armature shall keep the text colours at contrast ratio 4.5 to 1 against their background, shall pass the automated accessibility check on every page of the browser suite, and shall fit a phone screen 360 pixels wide without sideways scrolling of the page.

Rationale: Contrast and a narrow screen decide whether some people can read the page at all.

### NFR-4 Speak English and German
type: Non-functional
priority: should
verification: Inspection
component: Usability
refines: SN-11
issues: ARM-354, ARM-371
guide: getting-started

Armature shall show every label, message and relative time of the web app in English or in German, as the person's profile says.

Rationale: A German team should not read half of the page in English.

### NFR-5 Deliver a webhook promptly
type: Non-functional
priority: should
verification: Test
component: Reliability
refines: SN-7
issues: ARM-99
guide: notifications

Armature shall post the first delivery of an event within 10 seconds of the event on a healthy queue, and shall give up an attempt that is not answered within 10 seconds.

Rationale: A receiver that acts on events needs them while they still matter, and a hung receiver must not hold a worker.

### NFR-6 Resist replays and tampering with a delivery
type: Non-functional
priority: must
verification: Analysis
component: Security
refines: SN-7
issues: ARM-20
guide: integrations

Armature shall sign each delivery with the endpoint's secret in two forms, one over the body and one over the timestamp and the body, so that a receiver can refuse a delivery older than its own limit.

Rationale: A signature that says nothing about when it was made can be replayed for ever.
