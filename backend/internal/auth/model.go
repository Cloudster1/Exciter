package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// OrgRole is a member's standing within one organization.
type OrgRole string

const (
	RoleOwner  OrgRole = "owner"
	RoleAdmin  OrgRole = "admin"
	RoleMember OrgRole = "member"
	// RoleGuest is somebody from outside, let into the projects they are granted and nothing else.
	RoleGuest OrgRole = "guest"
)

// OrgRoles lists every role, in the order the API documents them.
var OrgRoles = []OrgRole{RoleOwner, RoleAdmin, RoleMember, RoleGuest}

// AssignableRoles are the roles an administrator gives; ownership is the bootstrap's alone.
var AssignableRoles = []OrgRole{RoleAdmin, RoleMember, RoleGuest}

// CanAdminister reports whether the role may change organization settings.
func (r OrgRole) CanAdminister() bool { return r == RoleOwner || r == RoleAdmin }

// RoleSource says who decided a member's role.
type RoleSource string

const (
	// RoleSourceManual is a role somebody here chose, which the identity
	// provider leaves alone while none of the person's groups is mapped.
	RoleSourceManual RoleSource = "manual"
	// RoleSourceProvider is a role the provider's groups gave, which a later
	// sign-in takes back when the group goes.
	RoleSourceProvider RoleSource = "oidc"
)

// RoleSources lists every source, in the order the API documents them.
var RoleSources = []RoleSource{RoleSourceManual, RoleSourceProvider}

// Member is somebody in the organization, as its administrators see them.
type Member struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	Role   OrgRole   `json:"role"`
	// RoleSource says whether the role follows the identity provider's groups.
	RoleSource RoleSource `json:"roleSource"`
	JoinedAt   time.Time  `json:"joinedAt"`
}

// Locale is a language the interface speaks.
type Locale string

const (
	// LocaleBrowser is no choice: the interface follows the browser.
	LocaleBrowser Locale = ""
	LocaleEnglish Locale = "en"
	LocaleGerman  Locale = "de"
)

// Locales lists every value a person may choose, in the order the API documents them.
var Locales = []Locale{LocaleBrowser, LocaleEnglish, LocaleGerman}

// User is a person, global across organizations.
type User struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
	// AvatarURL is where the picture is served from; empty means initials.
	AvatarURL string `json:"avatarUrl,omitempty"`
	// Locale is the interface language the person chose; empty means the browser's.
	Locale Locale `json:"locale"`
}

// CurrentOrg is the organization a session is acting in, with the caller's standing there.
type CurrentOrg struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
	Name string    `json:"name"`
	Role OrgRole   `json:"role"`
}

// Membership is a user's place in one organization.
type Membership struct {
	OrgID   uuid.UUID `json:"orgId"`
	OrgSlug string    `json:"orgSlug"`
	OrgName string    `json:"orgName"`
	Role    OrgRole   `json:"role"`
}

// Proof is how a session was opened, which decides how far it reaches.
type Proof string

const (
	ProofPassword Proof = "password"
	ProofOIDC     Proof = "oidc"
)

var (
	// ErrInvalidCredentials is returned for an unknown email and a wrong
	// password alike, so the login form cannot enumerate accounts.
	ErrInvalidCredentials = errors.New("invalid email or password")
	// ErrUserInactive is returned when a deactivated account signs in.
	ErrUserInactive = errors.New("account is deactivated")
	// ErrNotAMember is returned when somebody tries to act in an organization
	// they do not belong to, or names one that does not exist.
	ErrNotAMember = errors.New("not a member of that organization")
	// ErrSessionStaysHome is returned when a session proven for one
	// organization tries to act where that proof does not vouch for it.
	ErrSessionStaysHome = errors.New("this sign-in does not reach that organization")
	// ErrBadLocale is returned for a language the interface does not speak.
	ErrBadLocale = errors.New("choose a language from the list")
	// ErrNoSuchMember is returned when the person named is not a member here.
	ErrNoSuchMember = errors.New("that person is not a member of this organization")
	// ErrOwnerStays is returned when somebody tries to remove the owner or
	// change their role: the owner is how the organization is always entered again.
	ErrOwnerStays = errors.New("the owner of an organization stays its owner")
	// ErrRemoveSelf is returned when an administrator tries to remove themselves.
	ErrRemoveSelf = errors.New("you cannot remove yourself from the organization")
	// ErrChangeOwnRole is returned when an administrator tries to change their own role.
	ErrChangeOwnRole = errors.New("you cannot change your own role")
	// ErrBadRole is returned for a standing an administrator cannot give.
	ErrBadRole = errors.New("a person is made an admin, a member or a guest")
)
