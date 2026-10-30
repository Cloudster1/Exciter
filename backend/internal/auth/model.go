package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// OrgRole is a member's standing in the installation.
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

// CanAdminister reports whether the role may change the installation's settings.
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

// Member is somebody in the installation, as its administrators see them.
type Member struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	Role   OrgRole   `json:"role"`
	// RoleSource says whether the role follows the identity provider's groups.
	RoleSource RoleSource `json:"roleSource"`
	JoinedAt   time.Time  `json:"joinedAt"`
	// Active is false for somebody switched off: they sign in to nothing until an administrator switches them on.
	Active bool `json:"active"`
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

// User is a person with an account.
type User struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
	// AvatarURL is where the picture is served from; empty means initials.
	AvatarURL string `json:"avatarUrl,omitempty"`
	// Locale is the interface language the person chose; empty means the browser's.
	Locale Locale `json:"locale"`
	// Timezone is the IANA zone the person's dates are written in, such as
	// Europe/Berlin; empty means the browser's.
	Timezone string `json:"timezone"`
}

// Membership is a person's place in the installation.
type Membership struct {
	UserID uuid.UUID `json:"userId"`
	Role   OrgRole   `json:"role"`
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
	// ErrSwitchedOff is returned when somebody whose account is switched off, or who belongs to no installation,
	// signs in.
	ErrSwitchedOff = errors.New("account switched off")
	// ErrNoInstallation is returned when nothing has made the installation yet.
	ErrNoInstallation = errors.New("the installation has not been set up")
	// ErrBadLocale is returned for a language the interface does not speak.
	ErrBadLocale = errors.New("choose a language from the list")
	// ErrBadTimezone is returned for a zone the database of zones does not know.
	ErrBadTimezone = errors.New("choose a time zone from the list")
	// ErrNoSuchMember is returned when the person named is not a member here.
	ErrNoSuchMember = errors.New("that person is not a member here")
	// ErrOwnerStanding is returned when an owner's role is changed: owners are made by the bootstrap alone.
	ErrOwnerStanding = errors.New("an owner's role is not changed here")
	// ErrOwnerOnly is returned when somebody who is not an owner switches an owner off.
	ErrOwnerOnly = errors.New("only an owner can switch another owner off")
	// ErrLastAdministrator is returned when switching somebody off would leave nobody to administer the installation.
	ErrLastAdministrator = errors.New("the last active administrator cannot be switched off")
	// ErrSwitchSelfOff is returned when an administrator tries to switch their own account off.
	ErrSwitchSelfOff = errors.New("you cannot switch yourself off")
	// ErrChangeOwnRole is returned when an administrator tries to change their own role.
	ErrChangeOwnRole = errors.New("you cannot change your own role")
	// ErrBadRole is returned for a standing an administrator cannot give.
	ErrBadRole = errors.New("a person is made an admin, a member or a guest")
)
