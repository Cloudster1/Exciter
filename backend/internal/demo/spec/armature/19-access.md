# Functional requirements / Access

Who may sign in and what they may do.

### FR-ACC-1 Grant five roles by person or group
type: Functional
priority: must
verification: Test
component: Access
refines: SN-10
guide: access

When an administrator grants a role (global administrator, project administrator, scrum master, user or reader) to a person or a group over the installation or over one project, Armature shall allow exactly the actions that role lists, within that scope.

Rationale: A role that is only what it lists, with no inheritance, can be explained by looking at it.

### FR-ACC-2 Hide an issue from people who may not see it, and let them ask
type: Functional
priority: must
verification: Test
component: Access
refines: SN-10
issues: ARM-279
guide: access

When a person who may not see an issue opens it, Armature shall answer as if the issue did not exist, unless the project shows that issues exist, and shall let the person ask for access, telling the project's administrators only when the issue exists.

Rationale: A project is a wall, and the answer must not tell what is behind it; a request still has to reach someone.

### FR-ACC-3 Reset a forgotten password by mail
type: Functional
priority: should
verification: Test
component: Access
refines: SN-10
issues: ARM-104
guide: access

When a person with a local account asks for a password link, Armature shall answer the same whether or not the address is known, and shall mail a link only to a known one.

Rationale: A different answer tells a stranger which addresses have accounts.

### FR-ACC-4 Offer two-step sign-in and require it
type: Functional
priority: should
verification: Test
component: Access
refines: SN-10
issues: ARM-105
guide: access

When an administrator requires two-step sign-in for administrators or for everyone, Armature shall ask a person who has not added a second step to add one before it lets them work, and shall accept a recovery code in place of the app.

Rationale: A password alone is the weakest door of the installation.

### FR-ACC-5 Keep one active administrator
type: Functional
priority: must
verification: Test
component: Access
refines: SN-10
issues: ARM-408
guide: access

When two administrators are revoked at the same time, Armature shall keep at least one active global administrator.

Rationale: An installation nobody can administer cannot be repaired from within.

### FR-ACC-6 Let people in by provider group
type: Functional
priority: should
verification: Test
component: Access
refines: SN-10
issues: ARM-301, ARM-470
guide: access

When a person signs in through the identity provider and a provider group of theirs holds a role, Armature shall let them in as a member with that role, and the built-in group Everyone shall hold every member.

Rationale: Groups kept in the identity provider should not be kept twice by hand.
