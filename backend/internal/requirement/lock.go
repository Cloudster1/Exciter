package requirement

import "time"

// MaxLockReasonLength bounds why an administrator locked a requirement by hand (#205).
const MaxLockReasonLength = 500

// LockBy says what holds a requirement locked.
type LockBy string

const (
	LockByStatus LockBy = "status"
	LockByHand   LockBy = "hand"
)

// LockBys lists every LockBy, for the API document.
var LockBys = []LockBy{LockByStatus, LockByHand}

// Lock says why a requirement takes no new revision, move, link or trash; a hand lock is answered over a status one.
type Lock struct {
	By           LockBy     `json:"by"`
	Status       *StatusRef `json:"status"`
	Reason       string     `json:"reason"`
	LockedByName string     `json:"lockedByName"`
	LockedAt     *time.Time `json:"lockedAt"`
}

// LockInput locks a requirement by hand, or changes the reason of a hand lock already there.
type LockInput struct {
	Reason string `json:"reason"`
}
