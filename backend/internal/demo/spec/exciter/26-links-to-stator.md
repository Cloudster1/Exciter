# Functional requirements / Stator links

How requirements are traced to the pages of Stator and shown on them.

### FR-STA-1 Connect Stator
type: Functional
priority: must
verification: Test
component: Stator
refines: SN-9
issues: EXC-40
guide: links-to-stator

When an administrator names the address of a Stator, Exciter shall ask what it offers and whether it takes remote links on pages, and shall record the answer with the connection.

Rationale: Pages are only given links by a Stator that can take them.

### FR-STA-2 Link a requirement to a page
type: Functional
priority: must
verification: Test
component: Stator
refines: SN-9
relates: FR-STA-1
issues: EXC-41
guide: links-to-stator

When a person links a requirement to a page of Stator, Exciter shall show the page and its space, mark the link suspect when the page is published again, and follow the page when it moves or is deleted.

Rationale: A page that explains a requirement may change under it.

### FR-STA-3 Show the requirement on the page
type: Functional
priority: should
verification: Test
component: Stator
refines: SN-9
relates: FR-STA-2
issues: EXC-42, EXC-140
guide: links-to-stator

While Stator lists remote links on pages, Exciter shall put on each linked page one remote link per requirement, and shall answer the requirement lists that a block on a page asks for as the viewing person.

Rationale: Readers of a page should see the requirements it describes without leaving it.
