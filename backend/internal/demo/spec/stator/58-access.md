# Functional requirements / Permissions and access

Who may read and write, and how outsiders and the public come in.

### FR-ACC-1 Give access to a space by grid
type: Functional
priority: must
verification: Test
component: Permissions
refines: SN-6
issues: STA-19, STA-585
guide: permissions

Stator shall let administrators of a space give people and groups the permissions to view, add pages, add comments, delete and administer in it, and shall keep at least one administrator.

Rationale: Spaces hold what a team may share only with itself.

### FR-ACC-2 Restrict a page and the pages below it
type: Functional
priority: should
verification: Test
component: Permissions
refines: SN-6
issues: STA-19
guide: permissions

When an editor restricts who may view or edit a page, Stator shall apply the restriction to the page and every page below it, and shall refuse a restriction that would lock the editor out.

Rationale: One page of a space, such as a salary list, may need fewer readers than the rest.

### FR-ACC-3 Say why a person may or may not act
type: Functional
priority: should
verification: Demonstration
component: Permissions
refines: SN-6
issues: STA-81
guide: permissions

When an administrator of a space checks the access of a person to a page, Stator shall say whether the person may view, edit, delete and comment on it and which permission or restriction decides each.

Rationale: Nobody can fix a permission they cannot explain.

### FR-ACC-5 Let a guest into one space
type: Functional
priority: should
verification: Test
component: Guests
refines: SN-6
issues: STA-69
guide: guests

When an administrator invites a guest to a space with the right to read, comment or edit, Stator shall give the guest that right in that space alone and nothing of the organization, and shall record the invitation in the audit log.

Rationale: Customers and contractors need to work with a team without joining the organization.

### FR-ACC-6 Open a page to the public
type: Functional
priority: could
verification: Test
component: Public access
refines: SN-6
issues: STA-79, STA-80
guide: public-access

Where the organization allows it, Stator shall let anybody read the published pages of a space an administrator opened, and let an editor make a public link to a single page that shows the page alone and ends when it is revoked or runs out.

Rationale: A handbook for customers is read by people who have no account.
