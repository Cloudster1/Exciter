// Package attachment keeps the files attached to requirements (#21), as Stator
// keeps a page's; docs/api-contract-m1.md says how.
package attachment

import (
	"time"

	"github.com/google/uuid"
)

// DefaultMaxSize is the upload limit unless EXCITER_UPLOAD_LIMIT says otherwise.
const DefaultMaxSize int64 = 50 << 20

// Attachment is one file attached to a requirement.
type Attachment struct {
	ID            uuid.UUID `json:"id"`
	RequirementID uuid.UUID `json:"requirementId"`
	FileName      string    `json:"fileName"`
	ContentType   string    `json:"contentType"`
	Size          int64     `json:"size"`
	// Width and Height are an image's, in pixels.
	Width          *int      `json:"width"`
	Height         *int      `json:"height"`
	UploadedByName string    `json:"uploadedByName"`
	CreatedAt      time.Time `json:"createdAt"`
	// Referenced says a revision's statement or rationale shows it, which keeps its bytes.
	Referenced bool `json:"referenced"`
}
