package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// OrgRole is a member's standing: whether they are in, as a member or as a guest. What they may do comes from
// grants only.
type OrgRole string

const (
	RoleMember OrgRole = "member"
	// RoleGuest is somebody from outside, let into the projects they are granted and nothing else.
	RoleGuest OrgRole = "guest"
	// RoleAdmin is no standing: adding or letting somebody in as admin makes them a member and grants them the
	// built-in administrator role besides.
	RoleAdmin OrgRole = "admin"
)

// OrgRoles lists every standing, in the order the API documents them.
var OrgRoles = []OrgRole{RoleMember, RoleGuest}

// AssignableRoles are what an administrator adds somebody as.
var AssignableRoles = []OrgRole{RoleAdmin, RoleMember, RoleGuest}

// JoinRoles are what letting a waiting person in may make of them.
var JoinRoles = []OrgRole{RoleMember, RoleAdmin}

// AdministratorRole is the key of the built-in role that administers the installation.
const AdministratorRole = "administrator"

// Member is somebody in the installation, as its administrators see them.
type Member struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	// Role is the standing: member, or guest.
	Role     OrgRole   `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
	// Active is false for somebody switched off: they sign in to nothing until an administrator switches them on.
	Active bool `json:"active"`
	// Administers says a grant lets the person administer the installation.
	Administers bool `json:"administers"`
	// Roles are what the person holds across the installation, by name or through a group.
	Roles []MemberRole `json:"roles"`
}

// MemberRole is a role somebody holds across the installation, and the group it comes through, if any.
type MemberRole struct {
	Role string `json:"role"`
	Name string `json:"name"`
	// GroupID and GroupName name the group the grant is to; null for one by name.
	GroupID   *uuid.UUID `json:"groupId"`
	GroupName *string    `json:"groupName"`
	// GroupBuiltin is "everyone" for the built-in group that holds every member.
	GroupBuiltin *string `json:"groupBuiltin"`
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
	// ErrLastAdministrator is returned when switching somebody off would leave nobody to administer the installation.
	ErrLastAdministrator = errors.New("the last active administrator cannot be switched off")
	// ErrSwitchSelfOff is returned when an administrator tries to switch their own account off.
	ErrSwitchSelfOff = errors.New("you cannot switch yourself off")
	// ErrChangeOwnRole is returned when an administrator tries to change their own role.
	ErrChangeOwnRole = errors.New("you cannot change your own role")
	// ErrBadRole is returned for a standing an administrator cannot give.
	ErrBadRole = errors.New("a person is made a member or a guest; administering is a grant of the administrator role")
	// ErrBadNewRole is returned for what an administrator cannot add somebody as.
	ErrBadNewRole = errors.New("a person is added as an admin, a member or a guest")
)
