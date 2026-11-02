# Functional requirements / Import and export

How content leaves Stator and comes in.

### FR-EXP-1 Export a page as a document
type: Functional
priority: should
verification: Test
component: Export
refines: SN-9
issues: STA-86, STA-87, STA-321
guide: moving-pages

When a person exports a page they may read, Stator shall produce a PDF or a Word document of it, with the organization's logo and footer line.

Rationale: A page is handed to people outside the wiki as a document.

### FR-EXP-2 Exchange pages as Markdown
type: Functional
priority: should
verification: Test
component: Export
refines: SN-9
issues: STA-90, STA-500
guide: moving-pages

Stator shall export a page as Markdown and import Markdown as a page, and shall refuse a file whose content it cannot import with a sentence that says why.

Rationale: Developers keep their text in Markdown files.
