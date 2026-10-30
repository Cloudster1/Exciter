// Package perm decides who may do what: roles and the permissions each holds, groups, and the grants of a role to
// a person or a group across the installation or in one project (#12, #471).
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

// EveryoneGroup is the mark of the built-in group that holds every member.
const EveryoneGroup = "everyone"

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

// OrgCan is what the caller may do across the installation.
type OrgCan struct {
	// CreateProject is making projects, which a grant of createProjects gives.
	CreateProject bool `json:"createProject"`
	// Administer is the installation's settings, people, access and link types.
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

// Person is a member of the installation, as a picker offers them.
type Person struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// Group is a group of the installation.
type Group struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	// Source says who keeps the members: "local" by hand, "oidc" the identity provider, whose memberships every
	// sign-in replaces.
	Source string `json:"source"`
	// ExternalRef is the name in the provider's groups claim that fills the group; empty for one kept by hand.
	ExternalRef string `json:"externalRef"`
	// Builtin marks a group the installation keeps itself: "everyone" holds every member.
	Builtin     string `json:"builtin,omitempty"`
	MemberCount int    `json:"memberCount"`
	// RoleCount is how many grants name the group.
	RoleCount int `json:"roleCount"`
	// FromProvider groups mirror the identity provider's.
	FromProvider bool `json:"fromProvider"`
	// Members are the people in it, when one group is read.
	Members []Person `json:"members,omitempty"`
}

// StepKind is one condition a right depends on.
type StepKind string

const (
	// StepOrgAdmin is administering the installation, which a grant across it gives and which holds every right in
	// every project.
	StepOrgAdmin StepKind = "orgAdmin"
	// StepGuest is being a guest, whom only grants in the project reach, never those across the installation, and
	// whom no grant lets run the project.
	StepGuest StepKind = "guest"
	// StepGrant is holding a role, in the project or across the installation, that gives the right.
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
	// Grants are those that reach the person and give the right.
	Grants []Assignment `json:"grants"`
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
	Person Person       `json:"person"`
	Role   auth.OrgRole `json:"role"`
	// Administers says a grant lets the person administer the installation.
	Administers bool          `json:"administers"`
	Rights      []AccessRight `json:"rights"`
}
