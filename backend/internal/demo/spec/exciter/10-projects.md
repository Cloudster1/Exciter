# Functional requirements / Projects

How a team starts a project and says who may do what in it.

### FR-PRJ-1 Make a project from a template
type: Functional
priority: must
verification: Test
component: Projects
refines: SN-3
issues: EXC-11, EXC-24
guide: projects

When a person who may create projects gives a name and a key and picks a template, Exciter shall make the project with the types, attributes, link types and starter sections the template names, and shall leave the project unchanged when the template later changes.

Rationale: A team should start from a structure that fits its kind of specification, and a later change to a template must not rewrite projects already in use.

### FR-PRJ-2 Grant roles to people and groups
type: Functional
priority: must
verification: Test
component: Projects
refines: SN-4
relates: FR-PRJ-1
issues: EXC-12, EXC-471
guide: projects

When a project administrator grants a role to a person or a group, Exciter shall give the person the union of the permissions of every grant that reaches them, and shall refuse every action the union does not hold, whoever asks and whichever way the request comes.

Rationale: Rights are the only thing between a requirement and a change nobody agreed; they must hold for the browser, the API and an assistant alike.

### FR-PRJ-3 Archive a project
type: Functional
priority: should
verification: Test
component: Projects
refines: SN-4
relates: FR-PRJ-2
issues: EXC-11
guide: projects

When a project administrator archives a project, Exciter shall keep it readable, shall refuse every change to it, and shall remove it from the project list until it is unarchived.

Rationale: A finished project stays evidence; it must not change by accident.

### FR-PRJ-4 Sign in through the suite's identity provider
type: Functional
priority: must
verification: Test
component: Projects
refines: SN-15
issues: EXC-7, EXC-102
guide: projects

When a person chooses single sign-on, Exciter shall sign them in through the suite's identity provider, finish the sign-in only in the browser that started it, and make no second form necessary when the provider already has their session.

Rationale: People of the suite sign in once, and nobody may be signed in to somebody else's browser.
