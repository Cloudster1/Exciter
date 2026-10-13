// Package attachment keeps the files attached to requirements (#21), as Stator keeps a page's: the bytes in the
// object store, and what is known about them in the database. docs/api-contract-m1.md says how.
package attachment

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The units a limit is written in.
const (
	kib = 1 << 10
	mib = 1 << 20
)

// DefaultMaxSize is the upload limit unless EXCITER_UPLOAD_LIMIT says otherwise, Stator's and Armature's default.
const DefaultMaxSize int64 = 50 * mib

// Attachment is one file attached to a requirement.
type Attachment struct {
	ID            uuid.UUID `json:"id"`
	RequirementID uuid.UUID `json:"requirementId"`
	// FileName is the name it was uploaded with, which a download keeps.
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	// Width and Height are an image's, in pixels; null for anything else.
	Width          *int      `json:"width"`
	Height         *int      `json:"height"`
	UploadedByName string    `json:"uploadedByName"`
	CreatedAt      time.Time `json:"createdAt"`
	// Referenced says a revision's statement or rationale shows it, which keeps its bytes.
	Referenced bool `json:"referenced"`
}

var (
	// ErrNotFound is also the answer for a file on a requirement the caller may not see, or one taken off the list.
	ErrNotFound = errors.New("attachment not found")
	// ErrForbidden is somebody who views a requirement changing its files, which its editors do.
	ErrForbidden = errors.New("only the project's editors change a requirement's files")
	// ErrTooLarge is what every TooLargeError is.
	ErrTooLarge = errors.New("that file is too large")
	// ErrEmpty is an upload with no bytes in it.
	ErrEmpty = errors.New("that file is empty")
)

// TooLargeError refuses a file over the limit, naming the limit.
type TooLargeError struct{ Limit int64 }

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("That file is too large: a requirement takes files of up to %s. Make it smaller, or split it.", Size(e.Limit))
}

func (e *TooLargeError) Is(target error) bool { return target == ErrTooLarge }

// Size writes a byte count the way a person reads a limit: 50 MB, 512 KB.
func Size(n int64) string {
	switch {
	case n >= mib && n%mib == 0:
		return fmt.Sprintf("%d MB", n/mib)
	case n >= mib:
		return fmt.Sprintf("%.1f MB", float64(n)/mib)
	case n >= kib:
		return fmt.Sprintf("%d KB", n/kib)
	}
	return fmt.Sprintf("%d bytes", n)
}
