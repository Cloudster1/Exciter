// Package perm decides who may read, change and approve what in a project
// (#12); docs/api-contract-m1.md says how.
package perm

import (
	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

// Limits of the people and group pickers.
const (
	DefaultPickerLimit = 20
	MaxPickerLimit     = 50
)

// EveryoneName is how an answer names the everyone subject.
const EveryoneName = "Everyone"

// SubjectType says whom a grant names.
type SubjectType string

const (
	SubjectUser  SubjectType = "user"
	SubjectGroup SubjectType = "group"
	// SubjectEveryone is every member of the organization, guests left out.
	SubjectEveryone SubjectType = "everyone"
)

// SubjectTypes lists every SubjectType, for the API document.
var SubjectTypes = []SubjectType{SubjectUser, SubjectGroup, SubjectEveryone}

// Subject is somebody a grant names, as a list shows them.
type Subject struct {
	Type SubjectType `json:"type"`
	// ID is the person's or the group's, null for everyone.
	ID   *uuid.UUID `json:"id"`
	Name string     `json:"name"`
}

// SubjectRef names a subject in a request.
type SubjectRef struct {
	Type SubjectType `json:"type"`
	// ID is the person's or the group's, left out for everyone.
	ID *uuid.UUID `json:"id,omitempty"`
}

// Role is what a grant gives in a project.
type Role string

const (
	// RoleViewer reads the project's requirements, their history and links.
	RoleViewer Role = "viewer"
	// RoleReviewer reads, comments and approves revisions (#35).
	RoleReviewer Role = "reviewer"
	// RoleEditor reads, comments and changes requirements.
	RoleEditor Role = "editor"
	// RoleAdmin holds every right, settings and grants included; never everyone's or a guest's.
	RoleAdmin Role = "admin"
)

// Roles lists every Role, the narrowest first.
var Roles = []Role{RoleViewer, RoleReviewer, RoleEditor, RoleAdmin}

// Right is something a person may do in a project.
type Right string

const (
	RightView       Right = "view"
	RightComment    Right = "comment"
	RightEdit       Right = "edit"
	RightApprove    Right = "approve"
	RightAdminister Right = "administer"
)

// Rights lists every Right, in the order the inspector explains them.
var Rights = []Right{RightView, RightComment, RightEdit, RightApprove, RightAdminister}

// Grant is the role one subject holds in a project.
type Grant struct {
	Subject Subject `json:"subject"`
	Role    Role    `json:"role"`
}

// GrantInput is one row of a project's grants.
type GrantInput struct {
	Subject SubjectRef `json:"subject"`
	Role    Role       `json:"role"`
}

// GrantsInput replaces a project's whole table of grants.
type GrantsInput struct {
	Grants []GrantInput `json:"grants"`
}

// OrgCan is what the caller may do across the organization.
type OrgCan struct {
	// CreateProject is making projects: owners and administrators.
	CreateProject bool `json:"createProject"`
	// Administer is the organization's settings, people and link types.
	Administer bool `json:"administer"`
}

// ProjectCan is what the caller may do in one project, so the interface
// offers only that; an archived project allows only view.
type ProjectCan struct {
	View       bool `json:"view"`
	Comment    bool `json:"comment"`
	Edit       bool `json:"edit"`
	Approve    bool `json:"approve"`
	Administer bool `json:"administer"`
	// Purge is deleting from the trash for good, which administer gives.
	Purge bool `json:"purge"`
}

// Person is a member of the organization, as a picker offers them.
type Person struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// Group is a group of the organization, as a picker offers it.
type Group struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	MemberCount int       `json:"memberCount"`
	// FromProvider groups mirror the identity provider's.
	FromProvider bool `json:"fromProvider"`
}

// StepKind is one condition a right depends on.
type StepKind string

const (
	// StepOrgAdmin is administering the organization, which holds every
	// right in every project.
	StepOrgAdmin StepKind = "orgAdmin"
	// StepGuest is being a guest, whom only grants naming them or their
	// groups reach, never everyone's.
	StepGuest StepKind = "guest"
	// StepGrant is holding a role in the project that gives the right.
	StepGrant StepKind = "grant"
	// StepArchived is the project being archived, which allows only view.
	StepArchived StepKind = "archived"
)

// StepKinds lists every StepKind, for the API document.
var StepKinds = []StepKind{StepOrgAdmin, StepGuest, StepGrant, StepArchived}

// AccessStep is one condition, whether the person meets it, and why.
type AccessStep struct {
	Kind   StepKind `json:"kind"`
	Passed bool     `json:"passed"`
	// Grants are the project's grants that reach the person and give the right.
	Grants []Grant `json:"grants"`
}

// AccessRight is one right, the database's verdict and the steps behind it;
// the first step not passed is the one that decides a no.
type AccessRight struct {
	Right   Right        `json:"right"`
	Allowed bool         `json:"allowed"`
	Steps   []AccessStep `json:"steps"`
}

// AccessReport is what one person may do in one project, and why.
type AccessReport struct {
	Person     Person          `json:"person"`
	Role       auth.OrgRole    `json:"role"`
	RoleSource auth.RoleSource `json:"roleSource"`
	Rights     []AccessRight   `json:"rights"`
}
