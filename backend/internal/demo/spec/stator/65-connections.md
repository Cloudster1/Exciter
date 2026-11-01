# Functional requirements / Connections to the other tools

How pages show the issues of Armature and the requirements of Exciter, and how the tools find the pages.

### FR-CON-1 Connect Armature and one's own account
type: Functional
priority: must
verification: Test
component: Armature
refines: SN-7
issues: STA-27, STA-428, STA-430
guide: armature

When an administrator connects an Armature installation, and a person connects their account to it with a token or at sign-in, Stator shall ask Armature as that person from then on.

Rationale: A page must never show an issue its reader could not open in Armature.

### FR-CON-2 Show issues in a page
type: Functional
priority: must
verification: Test
component: Armature
refines: SN-7
issues: STA-28, STA-29, STA-30, STA-426
guide: armature

When an author types the key of an Armature issue or pastes its address, Stator shall show a chip with the issue's status, and shall offer an issue block and a list of issues from a query; for an issue the reader may not see, Stator shall say that it is not visible or gone.

Rationale: The state of the work belongs next to the text that talks about it.

### FR-CON-4 Tell Armature which pages name an issue
type: Functional
priority: should
verification: Test
component: Armature
refines: SN-7
issues: STA-32, STA-33
guide: armature

When a published page names an Armature issue, Stator shall put a remote link to the page on the issue, take it off when the page no longer names it, and refresh the issue's chip when Armature reports a change.

Rationale: Developers look at the issue; they should find the page from there.

### FR-CON-5 Show Exciter requirements in a page
type: Functional
priority: should
verification: Test
component: Exciter
refines: SN-7
issues: STA-440, STA-462
guide: exciter

When an author whose Exciter account is connected types the key of a requirement, Stator shall show a chip with its status, and shall offer a requirement block and a list from a query, asked as the reader.

Rationale: Requirements are written about in pages; the page should show the current wording.

### FR-CON-6 Keep the links of other tools on a page
type: Functional
priority: should
verification: Test
component: Remote links
refines: SN-7
issues: STA-419
guide: exciter

When another tool puts a remote link on a page as a person who may edit it, Stator shall show the link to the readers grouped by the tool, replace a link with the same address, and let an editor take it off.

Rationale: Exciter traces requirements to pages; a link only Exciter knows would not show where writers look.
