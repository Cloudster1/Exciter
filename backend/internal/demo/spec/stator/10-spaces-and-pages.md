# Functional requirements / Spaces and pages

How a team keeps its pages: spaces, the page tree, the trash and the archive.

### FR-SPC-1 Make a space with a home page
type: Functional
priority: must
verification: Test
component: Spaces
refines: SN-2
issues: STA-9
guide: spaces-and-pages

When a person who may create spaces makes a space with a key and a name, Stator shall make the space with a home page and let its administrators change its name and settings.

Rationale: Every team starts from a space; the home page is where its readers land.

### FR-SPC-2 Arrange pages in a tree
type: Functional
priority: must
verification: Test
component: Page tree
refines: SN-2
issues: STA-10, STA-36
guide: page-tree

When a person who may add pages creates a page or a folder below another, moves it by dragging, or copies it, Stator shall show it there in the page tree of the space, with the pages below it.

Rationale: A tree is how a team finds a page without remembering its name.

### FR-SPC-3 Restore a deleted page from the trash
type: Functional
priority: must
verification: Test
component: Page tree
refines: SN-2
issues: STA-11
guide: page-tree

When a person who may delete a page deletes it, Stator shall keep it with its pages below in the trash of the space, and shall bring it back with its place in the tree when a person restores it.

Rationale: A deleted page is often the wrong click; losing a page with its history would be costly.
