package requirement

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// KeyPattern is a requirement's key: its project's key, -R and its number, such as CHK-R42.
const KeyPattern = `^([A-Z][A-Z0-9]{1,9})-R([1-9][0-9]*)$`

var keyRe = regexp.MustCompile(KeyPattern)

// Key is the key of requirement number in the project keyed projectKey.
func Key(projectKey string, number int) string {
	return fmt.Sprintf("%s-R%d", projectKey, number)
}

// ParseKey splits a key, matched without regard to case, into its project's key and its number.
func ParseKey(key string) (string, int, bool) {
	m := keyRe.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(key)))
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}
