# Functional requirements / Writing together

The editor, writing at the same time, templates and files.

### FR-EDT-1 Write with formatting and blocks
type: Functional
priority: must
verification: Demonstration
component: Editor
refines: SN-3
issues: STA-12, STA-16, STA-41
guide: showcase

Stator shall let an editor format text and add headings, lists, tables, code, panels, expand blocks and a table of contents to a page from the toolbar or the slash menu.

Rationale: A wiki page is read by people; structure and emphasis make it readable.

### FR-EDT-2 Write on one draft together
type: Functional
priority: must
verification: Test
component: Editor
refines: SN-3
issues: STA-65
guide: editing-together

When several people who may edit a page open its editor at the same time, Stator shall merge their changes into one shared draft and show each person's caret and avatar to the others.

Rationale: Two people who edit one page one after the other overwrite each other's work.

### FR-TPL-1 Start a page from a template
type: Functional
priority: should
verification: Test
component: Templates
refines: SN-3
issues: STA-15, STA-62
guide: templates

When a person makes a page from a template, Stator shall fill the page with the template's content, show its hints until the person writes over them, and remove the hints that are left when the page is published.

Rationale: Pages a team writes again and again should look alike.

### FR-TPL-2 Keep templates of the organization and of a space
type: Functional
priority: should
verification: Test
component: Templates
refines: SN-3
issues: STA-63, STA-64
guide: templates

Stator shall let administrators of the organization keep templates that every space offers and administrators of a space keep templates that only that space offers, a template asking for variables that the person making a page fills in.

Rationale: A team's own forms belong to the team and to nobody else.

### FR-ATT-1 Attach files to a page
type: Functional
priority: should
verification: Test
component: Attachments
refines: SN-3
issues: STA-20, STA-93, STA-61
guide: attachments

When an editor attaches a file to a page, Stator shall keep it with the page, show images in the text, and preview the file where its type allows.

Rationale: A page about a design is no use without the design.
