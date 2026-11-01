# Functional requirements / Search and labels

How a reader finds a page.

### FR-SRC-1 Search pages, comments and files
type: Functional
priority: must
verification: Test
component: Search
refines: SN-5
issues: STA-18
guide: search

When a person searches for words, a phrase in quotes or words to leave out, Stator shall list the pages, comments and file names that match, best match or newest change first, and shall let the person narrow them by space, type, label, author and date.

Rationale: A wiki that cannot be searched is a pile.

### FR-SRC-2 Search shows only what the searcher may read
type: Functional
priority: must
verification: Test
component: Search
refines: SN-5
issues: STA-370, STA-598
guide: search

Stator shall list in a search only the pages the searching person may view, and shall find a page when a word of the search is mistyped by a letter.

Rationale: A search result is a way to read a page; a restricted page must not leak through it.

### FR-SRC-3 Label a page
type: Functional
priority: should
verification: Test
component: Labels
refines: SN-5
issues: STA-17
guide: labels

When an editor adds a label to a page, Stator shall show it on the page, offer the labels already in use, and let the editor take it off again.

Rationale: Labels cut across the tree, which a page can have only one place in.

### FR-SRC-4 List the pages of a label
type: Functional
priority: should
verification: Test
component: Labels
refines: SN-5
issues: STA-17, STA-55
guide: labels

When a person opens a label or asks for its pages, Stator shall list the published pages that carry it and that the person may read.

Rationale: A label is only useful if it brings its pages together.
