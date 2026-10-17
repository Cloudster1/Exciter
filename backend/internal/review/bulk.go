package review

import "github.com/cloudster1/exciter/backend/internal/requirement"

// BulkRequestItem names a requirement and the revision the caller saw, which its review pins.
type BulkRequestItem struct {
	Key      string `json:"key"`
	Revision int    `json:"revision"`
}

// BulkRequestInput asks for the review of several requirements of a project, each on its own (#171).
type BulkRequestInput struct {
	Items   []BulkRequestItem `json:"items"`
	Comment string            `json:"comment,omitempty"`
}

// BulkRequestResult is one requirement's outcome: the review asked for, or the refusal.
type BulkRequestResult struct {
	Key     string                   `json:"key"`
	Outcome requirement.BulkOutcome  `json:"outcome"`
	Review  *Review                  `json:"review"`
	Error   *requirement.BulkRefusal `json:"error"`
}
