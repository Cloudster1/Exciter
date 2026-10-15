// Package webhook is what Exciter's receivers of Armature's and Stator's webhooks share: the envelope version (#103).
package webhook

import "strconv"

// Version is the envelope version Exciter reads, and the one it sends once it sends webhooks of its own. It rises
// only for a change that breaks a receiver, as docs/integration.md says.
const Version = 1

// Reads says whether Exciter reads an envelope that names version v; 0 is a sender from before versions.
func Reads(v int) bool { return v == 0 || v == Version }

// ReadsHeader is Reads for the version header, which a receiver checks before parsing the body; an absent header is
// a sender from before versions.
func ReadsHeader(h string) bool { return h == "" || h == strconv.Itoa(Version) }
