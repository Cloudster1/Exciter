# Functional requirements / Drafts, publishing and history

How a page goes from a draft to what readers see, and how its versions are kept.

### FR-DRF-1 Keep a draft apart from the published page
type: Functional
priority: must
verification: Test
component: Editor
refines: SN-1
issues: STA-13
guide: drafts-and-history

While an author edits a published page, Stator shall save the author's changes as a draft that only the author sees, and shall show readers the page as it was last published until the author publishes the draft.

Rationale: A page that changes under its readers while it is being written cannot be trusted.

### FR-DRF-2 Publish with a note for the history
type: Functional
priority: must
verification: Test
component: Editor
refines: SN-1
issues: STA-13
guide: drafts-and-history

When an author publishes a draft, Stator shall make it the next version of the page, keep the note the author gave with it, and tell the page's watchers if the author chose to.

Rationale: The note says why a version exists, which a later reader of the history cannot guess.

### FR-DRF-3 Never publish over another author
type: Functional
priority: should
verification: Test
component: Editor
refines: SN-1
issues: STA-13
guide: drafts-and-history

If somebody published the page after the author's draft began, Stator shall not publish the draft, and shall offer to compare it with the new version or to keep it and publish it anyway.

Rationale: Silent overwriting loses the other author's work.

### FR-DRF-4 Compare two versions of a page
type: Functional
priority: should
verification: Demonstration
component: History
refines: SN-1
issues: STA-14
guide: drafts-and-history

When a reader picks two versions of a page, Stator shall show what the later one added and what it removed.

Rationale: A reader judges a change by what is different, not by rereading the page.

### FR-DRF-5 Restore an earlier version
type: Functional
priority: should
verification: Test
component: History
refines: SN-1
issues: STA-14
guide: drafts-and-history

When an editor restores an earlier version, Stator shall publish it again as the newest version and keep every version in the history.

Rationale: Undoing a bad change must not destroy the record of it.

### FR-DRF-7 Keep working pages live
type: Functional
priority: could
verification: Demonstration
component: Editor
refines: SN-1
issues: STA-70
guide: live-pages

Where an editor makes a page live, Stator shall turn what its editors type into the published page about a second later, keep one version in the history for each ten minutes of work, and record the switch in the audit log.

Rationale: A running agenda or a shared list is never finished, so a draft only gets in its way.

### FR-DRF-8 Hold pages to a review
type: Functional
priority: should
verification: Test
component: Review
refines: SN-11
issues: STA-313
guide: review-and-confirmation

Where a space or a branch of its pages is held to a review, Stator shall refuse to publish a draft of a page there directly, accept it for review by the named people and groups, and publish it as the next version once the number of approvals the rule asks for is reached.

Rationale: A procedure that goes out unchecked can do harm that a second pair of eyes would have stopped.

### FR-DRF-9 Ask readers to confirm a page
type: Functional
priority: could
verification: Test
component: Review
refines: SN-11
issues: STA-313
guide: review-and-confirmation

When an editor asks the readers of a page to confirm it, Stator shall record who confirmed which version and show the editors who of the people who may read it has not.

Rationale: The owner of a policy needs to know that it was read.
