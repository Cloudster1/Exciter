package auth

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGenerateTokenIsUniqueAndHashed(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		secret, digest, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		if seen[secret] {
			t.Fatal("GenerateToken returned a duplicate secret")
		}
		seen[secret] = true
		if len(digest) != 32 || !bytes.Equal(digest, HashToken(secret)) {
			t.Fatal("the digest is not the SHA-256 of the secret")
		}
	}
}

func TestAPITokensArePrefixedAndHashedWhole(t *testing.T) {
	secret, digest, err := GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "exciter_pat_") || len(secret) != len("exciter_pat_")+43 {
		t.Fatalf("token %q is not exciter_pat_ and 43 characters", secret)
	}
	if !bytes.Equal(digest, HashToken(secret)) {
		t.Fatal("the digest is not the SHA-256 of the whole token, prefix included")
	}
	if !IsAPIToken(secret) || ParseAPIToken(secret) != nil {
		t.Fatalf("a token just made does not parse: %q", secret)
	}
}

func TestParseAPITokenRefusesAnythingElse(t *testing.T) {
	good, _, _ := GenerateAPIToken()
	session, _, _ := GenerateToken()
	for name, secret := range map[string]string{
		"empty":           "",
		"a session":       session,
		"no body":         APITokenPrefix,
		"short":           good[:len(good)-1],
		"long":            good + "A",
		"padded":          good + "=",
		"standard base64": APITokenPrefix + "+/" + good[len(APITokenPrefix)+2:],
		"another prefix":  "stator_pat_" + strings.TrimPrefix(good, APITokenPrefix),
		"upper prefix":    strings.ToUpper(APITokenPrefix) + strings.TrimPrefix(good, APITokenPrefix),
	} {
		if err := ParseAPIToken(secret); err != ErrInvalidToken {
			t.Errorf("%s %q parsed: %v", name, secret, err)
		}
	}
	if IsAPIToken(session) {
		t.Error("a session secret was taken for a personal access token")
	}
}

func TestOnlyTheReadAndEvidenceScopesExistAndOneAtATime(t *testing.T) {
	if err := ValidateScopes(nil); err != nil {
		t.Errorf("no scope: %v", err)
	}
	if err := ValidateScopes([]string{ScopeRead, ScopeRead}); err != nil {
		t.Errorf("read: %v", err)
	}
	if err := ValidateScopes([]string{ScopeEvidence}); err != nil {
		t.Errorf("evidence: %v", err)
	}
	for _, scope := range []string{"write", "admin", "READ", "", ScopeEvidence} {
		if err := ValidateScopes([]string{ScopeRead, scope}); err != ErrTokenScope {
			t.Errorf("%q was accepted: %v", scope, err)
		}
	}
}

func TestOnlyATokenMadeToReadIsReadOnlyAndOnlyALimitedOneIsLimited(t *testing.T) {
	id := uuid.New()
	full := &CallingToken{ID: id, Scopes: []string{}, AllProjects: true}
	reading := &CallingToken{ID: id, Scopes: []string{ScopeRead}, AllProjects: true}
	limited := &CallingToken{ID: id, Scopes: []string{}, Projects: []string{"CHK"}}
	for name, tc := range map[string]struct {
		p              *Principal
		readOnly, some bool
	}{
		"nobody":          {nil, false, false},
		"a session":       {&Principal{SessionID: &id}, false, false},
		"a full token":    {&Principal{Token: full}, false, false},
		"a reading token": {&Principal{Token: reading}, true, false},
		"a limited token": {&Principal{Token: limited}, false, true},
	} {
		if got := tc.p.ReadOnly(); got != tc.readOnly {
			t.Errorf("%s: ReadOnly = %v, want %v", name, got, tc.readOnly)
		}
		if got := tc.p.InProjectsOnly(); got != tc.some {
			t.Errorf("%s: InProjectsOnly = %v, want %v", name, got, tc.some)
		}
	}
}

func TestANewTokenIsValidatedBeforeItIsMade(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	s := &Service{now: func() time.Time { return now }}
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	for name, tc := range map[string]struct {
		in   NewAPIToken
		want error
	}{
		"fine":             {NewAPIToken{Name: " deploy ", Scopes: []string{ScopeRead, ScopeRead}, ExpiresAt: &future}, nil},
		"forever":          {NewAPIToken{Name: "deploy"}, nil},
		"no name":          {NewAPIToken{Name: "  "}, ErrTokenName},
		"a long name":      {NewAPIToken{Name: strings.Repeat("x", MaxTokenNameLength+1)}, ErrTokenName},
		"a long name fits": {NewAPIToken{Name: strings.Repeat("ü", MaxTokenNameLength)}, nil},
		"an unknown scope": {NewAPIToken{Name: "deploy", Scopes: []string{"write"}}, ErrTokenScope},
		"already expired":  {NewAPIToken{Name: "deploy", ExpiresAt: &past}, ErrTokenExpiry},
		"expiring now":     {NewAPIToken{Name: "deploy", ExpiresAt: &now}, ErrTokenExpiry},
	} {
		in := tc.in
		if err := s.validateNewToken(&in); err != tc.want {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	in := NewAPIToken{Name: " deploy ", Scopes: []string{ScopeRead, ScopeRead}}
	_ = s.validateNewToken(&in)
	if in.Name != "deploy" || len(in.Scopes) != 1 {
		t.Errorf("the name is trimmed and the scopes deduplicated: %+v", in)
	}
}

func TestATokenNamesItsProjectsOnceEach(t *testing.T) {
	s := &Service{now: time.Now}
	in := NewAPIToken{Name: "deploy", Projects: []string{" chk", "CHK", "sys", " "}}
	if err := s.validateNewToken(&in); err != nil {
		t.Fatal(err)
	}
	if strings.Join(in.Projects, ",") != "CHK,SYS" {
		t.Errorf("the keys are %v, want CHK and SYS once each", in.Projects)
	}
	all := NewAPIToken{Name: "deploy"}
	if err := s.validateNewToken(&all); err != nil || all.Projects == nil || len(all.Projects) != 0 {
		t.Errorf("a token naming no project = %v %v, want an empty list", all.Projects, err)
	}
	many := NewAPIToken{Name: "deploy"}
	for i := range MaxTokenProjects + 1 {
		many.Projects = append(many.Projects, fmt.Sprintf("P%d", i))
	}
	if err := s.validateNewToken(&many); err != ErrTokenProjects {
		t.Errorf("%d projects = %v, want %v", len(many.Projects), err, ErrTokenProjects)
	}
}
