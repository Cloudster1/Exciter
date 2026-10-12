package oidc

import (
	"strings"
)

// identityFromClaims reads the claims Exciter cares about out of a token that
// has already been verified.
func identityFromClaims(issuer, subject string, claims map[string]any, groupsClaim string) (*Identity, error) {
	identity := Identity{
		Issuer:  issuer,
		Subject: subject,
		Email:   strings.ToLower(strings.TrimSpace(asString(claims["email"]))),
		Name:    strings.TrimSpace(asString(claims["name"])),
	}
	if identity.Email == "" {
		return nil, ErrNoEmail
	}
	// An address the provider never checked is not the provider vouching for
	// the person, and the address is what a bootstrap account is matched on.
	switch verified := claims["email_verified"].(type) {
	case bool:
		if !verified {
			return nil, ErrEmailUnverified
		}
	case string:
		if strings.EqualFold(verified, "false") {
			return nil, ErrEmailUnverified
		}
	}
	if identity.Name == "" {
		given := strings.TrimSpace(asString(claims["given_name"]) + " " + asString(claims["family_name"]))
		identity.Name = given
	}
	if identity.Name == "" {
		identity.Name = strings.TrimSpace(asString(claims["preferred_username"]))
	}
	if identity.Name == "" {
		// Better a name derived from the address than a blank one in every
		// listing the person appears in.
		identity.Name, _, _ = strings.Cut(identity.Email, "@")
	}
	identity.Groups = normalizeGroups(asStrings(claimAt(claims, groupsClaim)))
	return &identity, nil
}

// claimAt finds a claim by its exact name, which may be a URL with dots in it,
// or else by a dotted path into nested objects, such as realm_access.roles.
func claimAt(claims map[string]any, name string) any {
	if v, ok := claims[name]; ok {
		return v
	}
	var current any = claims
	for _, part := range strings.Split(name, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// asStrings reads a claim that is a list of strings, tolerating a provider that
// sends a single string when somebody is in exactly one group.
func asStrings(v any) []string {
	switch value := v.(type) {
	case string:
		return []string{value}
	case []any:
		out := make([]string, 0, len(value))
		for _, each := range value {
			if s, ok := each.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return value
	default:
		return nil
	}
}

// normalizeGroups trims, drops blanks and repeats, and keeps the order sent.
func normalizeGroups(groups []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	return out
}

// diffGroups says which groups somebody joins and leaves so their membership
// becomes exactly the claimed set, or revoking access there would not revoke it here.
func diffGroups(current, claimed []string) (join, leave []string) {
	want := map[string]bool{}
	for _, g := range claimed {
		want[g] = true
	}
	have := map[string]bool{}
	for _, g := range current {
		have[g] = true
		if !want[g] {
			leave = append(leave, g)
		}
	}
	for _, g := range claimed {
		if !have[g] {
			join = append(join, g)
			have[g] = true
		}
	}
	return join, leave
}
